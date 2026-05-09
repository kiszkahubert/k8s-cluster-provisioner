package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"cluster-provisioner/internal/pve"
)

type ClusterForm struct {
	ClusterName  string
	CPCount      int
	WorkerCount  int
	CPMemory     int
	WorkerMemory int
	CPCores      int
	WorkerCores  int
	CiUser       string
	CiPassword   string
}

type ClusterNetwork struct {
	BaseIP  string
	CPStart int
	Gateway string
}

func main() {
	p, err := pve.New(pve.ClientConfig{
		Host:       "192.168.122.147:8006",
		User:       "root@pam",
		Password:   "kiszka123",
		TemplateID: 9000,
	})
	if err != nil {
		log.Fatal(err)
	}
	form := ClusterForm{
		ClusterName:  "my-prod-cluster",
		CPCount:      3,
		WorkerCount:  2,
		CPCores:      2,
		CPMemory:     2048,
		WorkerCores:  4,
		WorkerMemory: 4096,
		CiUser:       "kiszka",
		CiPassword:   "kiszka123",
	}
	network := ClusterNetwork{
		BaseIP:  "192.168.122",
		CPStart: 33,
		Gateway: "192.168.122.1",
	}
	cpIPs, workerIPs, vip, metallbRange := allocateIPs(network, form.CPCount, form.WorkerCount)
	fmt.Printf("VIP: %s\n", vip)
	fmt.Printf("MetalLB pool: %s\n", metallbRange)

	sshKey := getSSHPubKey()
	var vmsToCreate []pve.VMSpec
	for i := 1; i <= form.CPCount; i++ {
		vmsToCreate = append(vmsToCreate, pve.VMSpec{
			Name:       fmt.Sprintf("%s-cp-%02d", form.ClusterName, i),
			Cores:      form.CPCores,
			MemoryMB:   form.CPMemory,
			DiskSizeGB: 20,
			CiUser:     form.CiUser,
			CiPassword: form.CiPassword,
			SSHKey:     sshKey,
			StaticIP:   cpIPs[i-1] + "/24",
		})
	}
	for i := 1; i <= form.WorkerCount; i++ {
		vmsToCreate = append(vmsToCreate, pve.VMSpec{
			Name:       fmt.Sprintf("%s-worker-%02d", form.ClusterName, i),
			Cores:      form.WorkerCores,
			MemoryMB:   form.WorkerMemory,
			DiskSizeGB: 30,
			CiUser:     form.CiUser,
			CiPassword: form.CiPassword,
			SSHKey:     sshKey,
			StaticIP:   workerIPs[i-1] + "/24",
		})
	}
	ctx := context.Background()
	vmIPs := make(map[string]string)
	allIPs := append(cpIPs, workerIPs...)
	for i, spec := range vmsToCreate {
		_, err := p.ProvisionVM(ctx, "pve", spec)
		if err != nil {
			log.Fatalf("Err creating %s: %v", spec.Name, err)
		}
		vmIPs[spec.Name] = allIPs[i]
		fmt.Printf("%s's IP: %s\n", spec.Name, allIPs[i])
	}
	var cpSection, workerSection strings.Builder
	cpSection.WriteString("[control_plane]\n")
	workerSection.WriteString("[workers]\n")
	for name, ip := range vmIPs {
		line := fmt.Sprintf("%s ansible_user=%s node_name=%s\n", ip, form.CiUser, name)
		if strings.Contains(name, "-cp-") {
			cpSection.WriteString(line)
		} else if strings.Contains(name, "-worker-") {
			workerSection.WriteString(line)
		}
	}
	inventoryContent := fmt.Sprintf("%s\n%s\n[cluster:children]\ncontrol_plane\nworkers\n", cpSection.String(), workerSection.String())
	err = os.WriteFile("../Ansible/inventory.ini", []byte(inventoryContent), 0644)
	if err != nil {
		log.Fatalf("Error saving inventory.ini: %v", err)
	}
	varsContent := fmt.Sprintf("vip_address: %s\nmetallb_range: %s\n", vip, metallbRange)
	err = os.WriteFile("../Ansible/cluster_vars.yml", []byte(varsContent), 0644)
	if err != nil {
		log.Fatalf("Error saving cluster_vars.yml: %v", err)
	}
	fmt.Println("inventory.ini created")
}

func getSSHPubKey() string {
	homeDir, _ := os.UserHomeDir()
	keyPath := homeDir + "/.ssh/id_ed25519.pub"
	keyBytes, _ := os.ReadFile(keyPath)
	rawKey := strings.TrimSpace(string(keyBytes))
	return strings.ReplaceAll(url.QueryEscape(rawKey), "+", "%20")
}

func allocateIPs(net ClusterNetwork, cpCount, workerCount int) (cpIPs, workerIPs []string, vip string, metalLBRange string) {
	offset := net.CPStart
	for i := 0; i < cpCount; i++ {
		cpIPs = append(cpIPs, fmt.Sprintf("%s.%d", net.BaseIP, offset+i))
	}
	offset += cpCount
	for i := 0; i < workerCount; i++ {
		workerIPs = append(workerIPs, fmt.Sprintf("%s.%d", net.BaseIP, offset+i))
	}
	offset += workerCount
	vip = fmt.Sprintf("%s.%d", net.BaseIP, offset)
	offset++
	metalLBRange = fmt.Sprintf("%s.%d-%s.%d", net.BaseIP, offset, net.BaseIP, offset+19)
	return
}
