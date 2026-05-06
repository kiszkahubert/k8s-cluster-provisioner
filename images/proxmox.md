```
cd /var/lib/vz/template/iso

wget https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img

qm create 9000 --name node-base --memory 2048 --cores 2 --net0 virtio,bridge=vmbr0

qm importdisk 9000 /var/lib/vz/template/iso/noble-server-cloudimg-amd64.img local-lvm

qm set 9000 --scsihw virtio-scsi-pci --scsi0 local-lvm:vm-9000-disk-0

qm set 9000 --ide2 local-lvm:cloudinit

qm set 9000 --ipconfig0 ip=dhcp

qm set 9000 --boot c --bootdisk scsi0

qm set 9000 --serial0 socket --vga serial0

qm set 9000 --cicustom "user=local:snippets/base-image-init.yml"

qm resize 9000 scsi0 +7G

qm start 9000

qm set 9000 --delete cicustom

qm template 9000
```
