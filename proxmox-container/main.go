package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/ct"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/download"

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
		containerPassword := os.Getenv("CONTAINER_PASSWORD")

		if proxmoxUsername == "" || proxmoxPassword == "" || containerPassword == "" {
			return fmt.Errorf("PROXMOX_USERNAME, PROXMOX_PASSWORD and CONTAINER_PASSWORD must be set in the .env file")
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

		// Download LXC template
		ctTemplate, err := download.NewFile(ctx, "latest-ubuntu22-jammy-lxc", &download.FileArgs{
			ContentType: pulumi.String("vztmpl"),
			DatastoreId: pulumi.String("local"),
			NodeName:    pulumi.String("pve"),
			Url:         pulumi.String("https://images.linuxcontainers.org/images/ubuntu/jammy/amd64/default/20241101_07%3A42/rootfs.tar.xz"),
		}, pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Create a Container
		newCT, err := ct.NewContainer(ctx, "container", &ct.ContainerArgs{
			NodeName: pulumi.String("pve"),
			OperatingSystem: &ct.ContainerOperatingSystemArgs{
				TemplateFileId: ctTemplate.ID(),
				Type:           pulumi.String("ubuntu"),
			},
			Memory: &ct.ContainerMemoryArgs{
				Dedicated: pulumi.Int(4096),
				Swap:      pulumi.Int(4096),
			},
			Cpu: &ct.ContainerCpuArgs{
				Cores: pulumi.Int(2),
			},
			Disk: &ct.ContainerDiskArgs{
				DatastoreId: pulumi.String("local-lvm"),
				Size:        pulumi.Int(10),
			},
			NetworkInterfaces: ct.ContainerNetworkInterfaceArray{
				&ct.ContainerNetworkInterfaceArgs{
					Name:    pulumi.String("eth0"),
					Bridge:  pulumi.String("vmbr0"),
					Enabled: pulumi.Bool(true),
				},
			},
			Initialization: &ct.ContainerInitializationArgs{
				UserAccount: &ct.ContainerInitializationUserAccountArgs{
					Password: pulumi.String(containerPassword),
				},
			},
			VmId:    pulumi.Int(200),
			Started: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{ctTemplate}), pulumi.Provider(provider))
		if err != nil {
			return err
		}

		// Export the Container ID
		ctx.Export("containerId", newCT.ID())

		return nil
	})
}
