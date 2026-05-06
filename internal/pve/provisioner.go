package pve

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/luthermonson/go-proxmox"
)

type Config struct {
	Host       string
	User       string
	Password   string
	TemplateID uint
	VMID       uint
	Name       string
	Cores      int
	MemoryMB   int
	DiskSizeGB uint64
	CiUser     string
	CiPassword string
}

type Provisioner struct {
	client *proxmox.Client
	cfg    Config
}

func New(cfg Config) (*Provisioner, error) {
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

func (p *Provisioner) ProvisionVM(ctx context.Context, node string) (uint, error) {
	n, err := p.client.Node(ctx, node)
	if err != nil {
		return 0, err
	}
	template, err := n.VirtualMachine(ctx, int(p.cfg.TemplateID))
	if err != nil {
		return 0, err
	}
	vmid := p.cfg.VMID
	if vmid == 0 {
		cluster, err := p.client.Cluster(ctx)
		if err != nil {
			return 0, err
		}
		next, err := cluster.NextID(ctx)
		if err != nil {
			return 0, err
		}
		vmid = uint(next)
	}
	_, task, err := template.Clone(ctx, &proxmox.VirtualMachineCloneOptions{
		NewID:  int(vmid),
		Name:   p.cfg.Name,
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
		proxmox.VirtualMachineOption{Name: "cores", Value: p.cfg.Cores},
		proxmox.VirtualMachineOption{Name: "memory", Value: p.cfg.MemoryMB},
		proxmox.VirtualMachineOption{Name: "ciuser", Value: p.cfg.CiUser},
		proxmox.VirtualMachineOption{Name: "cipassword", Value: p.cfg.CiPassword},
	)
	if err != nil {
		return 0, err
	}
	err = waitTask(ctx, configTask)
	if err != nil {
		return 0, err
	}
	if p.cfg.DiskSizeGB > 0 {
		resizeTask, err := vm.ResizeDisk(ctx, "scsi0", fmt.Sprintf("%dG", p.cfg.DiskSizeGB))
		if err != nil {
			return 0, err
		}
		err = waitTask(ctx, resizeTask)
		if err != nil {
			return 0, err
		}
	}

	return vmid, nil
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
