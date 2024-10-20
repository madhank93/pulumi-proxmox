package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/download"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/vm"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		// Load environment variables from .env file
		err := godotenv.Load()
		if err != nil {
			return fmt.Errorf("error loading .env file:  %v", err)
		}

		// Load username and password
		proxmoxUsername := os.Getenv("PROXMOX_USERNAME")
		proxmoxPassword := os.Getenv("PROXMOX_PASSWORD")

		if proxmoxUsername == "" || proxmoxPassword == "" {
			return fmt.Errorf("PROXMOX_USERNAME and PROXMOX_PASSWORD must be set in the .env file")
		}

		// Configure the Proxmox provider
		provider, err := proxmoxve.NewProvider(ctx, "proxmoxve", &proxmoxve.ProviderArgs{
			Endpoint: pulumi.String("https://192.168.1.198:8006/"),
			Username: pulumi.String(proxmoxUsername),
			Password: pulumi.String(proxmoxPassword),
			Insecure: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		// Download ISO image
		iso, err := download.NewFile(ctx, "latest-ubuntu22-jammy-iso", &download.FileArgs{
			ContentType: pulumi.String("iso"),
			DatastoreId: pulumi.String("local"),
			NodeName:    pulumi.String("pve"),
			Url:         pulumi.String("https://releases.ubuntu.com/jammy/ubuntu-22.04.5-live-server-amd64.iso"),
		}, pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Create a VM
		newVM, err := vm.NewVirtualMachine(ctx, "vm", &vm.VirtualMachineArgs{
			NodeName: pulumi.String("pve"),
			Name:     pulumi.String("vm"),
			Cpu: &vm.VirtualMachineCpuArgs{
				Cores: pulumi.Int(2),
			},
			Memory: &vm.VirtualMachineMemoryArgs{
				Dedicated: pulumi.Int(4800),
				Floating:  pulumi.Int(4800),
			},
			Disks: &vm.VirtualMachineDiskArray{
				vm.VirtualMachineDiskArgs{
					Size:       pulumi.Int(25),
					Interface:  pulumi.String("scsi0"),
					Iothread:   pulumi.Bool(true),
					FileFormat: pulumi.String("raw"),
				},
			},
			Cdrom: vm.VirtualMachineCdromArgs{
				Enabled:   pulumi.Bool(true),
				FileId:    iso.ID(),
				Interface: pulumi.String("ide3"),
			},
			BootOrders: pulumi.StringArray{
				pulumi.String("scsi0"),
				pulumi.String("ide3"),
				pulumi.String("net0"),
			},
			ScsiHardware:    pulumi.String("virtio-scsi-single"),
			OperatingSystem: vm.VirtualMachineOperatingSystemArgs{Type: pulumi.String("l26")},
			NetworkDevices: vm.VirtualMachineNetworkDeviceArray{
				vm.VirtualMachineNetworkDeviceArgs{
					Model:  pulumi.String("virtio"),
					Bridge: pulumi.String("vmbr0"),
				},
			},
			OnBoot: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{iso}), pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Export the VM and Container IDs
		ctx.Export("vmId", newVM.ID())

		return nil
	})
}
