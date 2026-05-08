package pve

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/luthermonson/go-proxmox"
)

type ClientConfig struct {
	Host       string
	User       string
	Password   string
	TemplateID uint
}
type VMSpec struct {
	Name       string
	Cores      int
	MemoryMB   int
	DiskSizeGB uint64
	CiUser     string
	CiPassword string
	SSHKey     string
}

type Provisioner struct {
	client *proxmox.Client
	cfg    ClientConfig
}

func New(cfg ClientConfig) (*Provisioner, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	client := proxmox.NewClient(
		fmt.Sprintf("https://%s/api2/json", cfg.Host),
		proxmox.WithHTTPClient(httpClient),
		proxmox.WithCredentials(&proxmox.Credentials{
			Username: cfg.User,
			Password: cfg.Password,
		}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("Cannot connest to Proxmox: %w", err)
	}
	return &Provisioner{client: client, cfg: cfg}, nil
}

func (p *Provisioner) ProvisionVM(ctx context.Context, node string, spec VMSpec) (uint, error) {
	n, err := p.client.Node(ctx, node)
	if err != nil {
		return 0, err
	}
	template, err := n.VirtualMachine(ctx, int(p.cfg.TemplateID))
	if err != nil {
		return 0, err
	}
	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		return 0, err
	}
	nextID, err := cluster.NextID(ctx)
	if err != nil {
		return 0, err
	}
	vmid := uint(nextID)
	_, task, err := template.Clone(ctx, &proxmox.VirtualMachineCloneOptions{
		NewID:  int(vmid),
		Name:   spec.Name,
		Full:   1,
		Target: node,
	})
	if err != nil {
		return 0, err
	}
	err = waitTask(ctx, task)
	if err != nil {
		return 0, err
	}
	vm, err := n.VirtualMachine(ctx, int(vmid))
	if err != nil {
		return 0, fmt.Errorf("get new vm: %w", err)
	}
	configTask, err := vm.Config(ctx,
		proxmox.VirtualMachineOption{Name: "cores", Value: spec.Cores},
		proxmox.VirtualMachineOption{Name: "memory", Value: spec.MemoryMB},
		proxmox.VirtualMachineOption{Name: "ciuser", Value: spec.CiUser},
		proxmox.VirtualMachineOption{Name: "cipassword", Value: spec.CiPassword},
		proxmox.VirtualMachineOption{Name: "sshkeys", Value: spec.SSHKey},
		proxmox.VirtualMachineOption{Name: "protection", Value: 0},
	)
	if err != nil {
		return 0, err
	}
	err = waitTask(ctx, configTask)
	if err != nil {
		return 0, err
	}
	if spec.DiskSizeGB > 0 {
		resizeTask, err := vm.ResizeDisk(ctx, "scsi0", fmt.Sprintf("%dG", spec.DiskSizeGB))
		if err != nil {
			return 0, err
		}
		err = waitTask(ctx, resizeTask)
		if err != nil {
			return 0, err
		}
	}
	startTask, err := vm.Start(ctx)
	if err != nil {
		return 0, err
	}
	err = waitTask(ctx, startTask)
	if err != nil {
		return 0, err
	}
	return vmid, nil
}

func (p *Provisioner) DestroyVM(ctx context.Context, node string, vmid uint) error {
	n, err := p.client.Node(ctx, node)
	if err != nil {
		return err
	}
	vm, err := n.VirtualMachine(ctx, int(vmid))
	if err != nil {
		return err
	}
	stopTask, err := vm.Stop(ctx)
	if err == nil {
		_ = waitTask(ctx, stopTask)
	}
	deleteTask, err := vm.Delete(ctx)
	if err != nil {
		return fmt.Errorf("Falied to delete vm %d: %w", vmid, err)
	}
	return waitTask(ctx, deleteTask)
}

func (p *Provisioner) WaitForIP(ctx context.Context, node string, vmid uint) (string, error) {
	n, err := p.client.Node(ctx, node)
	if err != nil {
		return "", err
	}
	vm, err := n.VirtualMachine(ctx, int(vmid))
	if err != nil {
		return "", err
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
		ifaces, err := vm.AgentGetNetworkIFaces(ctx)
		if err != nil {
			continue
		}
		for _, iface := range ifaces {
			if iface.Name == "lo" {
				continue
			}
			for _, ip := range iface.IPAddresses {
				if ip.IPAddressType == "ipv4" && ip.IPAddress != "127.0.0.1" {
					return ip.IPAddress, nil
				}
			}
		}
	}
}

func waitTask(ctx context.Context, task *proxmox.Task) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		err := task.Ping(ctx)
		if err != nil {
			return err
		}
		if task.IsCompleted {
			if !task.IsSuccessful {
				return fmt.Errorf("%s", task.ExitStatus)
			}
			return nil
		}
	}
}
