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
		iso, err := download.NewFile(ctx, "latest-Ubuntu22-Jammy-Img", &download.FileArgs{
			ContentType: pulumi.String("iso"),
			DatastoreId: pulumi.String("local"),
			NodeName:    pulumi.String("pve"),
			Url:         pulumi.String("https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"),
		}, pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Create a VM
		newVM, err := vm.NewVirtualMachine(ctx, "vm", &vm.VirtualMachineArgs{
			NodeName: pulumi.String("pve"),
			Name:     pulumi.String("example-vm"),
			Cpu: &vm.VirtualMachineCpuArgs{
				Cores: pulumi.Int(2),
			},
			Memory: &vm.VirtualMachineMemoryArgs{
				Floating: pulumi.Int(3200),
			},
			Disks: &vm.VirtualMachineDiskArray{
				vm.VirtualMachineDiskArgs{
					Size:       pulumi.Int(30),
					Interface:  pulumi.String("virtio0"),
					FileFormat: pulumi.String("raw"),
					FileId:     iso.ID(),
				},
			},
			Initialization: vm.VirtualMachineInitializationArgs{
				UserAccount: vm.VirtualMachineInitializationUserAccountArgs{
					Username: pulumi.String("ubuntu"),
					Password: pulumi.String("ubuntu"),
				},
			},
			OperatingSystem: &vm.VirtualMachineOperatingSystemArgs{Type: pulumi.String("l26")},
			NetworkDevices: &vm.VirtualMachineNetworkDeviceArray{
				vm.VirtualMachineNetworkDeviceArgs{
					Model:  pulumi.String("virtio"),
					Bridge: pulumi.String("vmbr0"),
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{iso}), pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Export the VM and Container IDs
		ctx.Export("vmId", newVM.ID())

		return nil
	})
}
