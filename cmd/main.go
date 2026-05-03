package main

import (
	"context"
	"fmt"
	"log"

	"cluster-provisioner/internal/pve"
)

func main() {
	p, err := pve.New(pve.Config{
		Host:       "192.168.122.243:8006",
		User:       "root@pam",
		Password:   "kiszka123",
		TemplateID: 9000,
		Name:       "test-vm-01",
		Cores:      2,
		MemoryMB:   2048,
		DiskSizeGB: 20,
	})
	if err != nil {
		log.Fatal(err)
	}
	vmid, err := p.ProvisionVM(context.Background(), "pve")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("VM created, VMID: %d\n", vmid)
}
