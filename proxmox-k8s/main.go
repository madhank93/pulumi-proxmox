package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/download"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/storage"
	"github.com/muhlba91/pulumi-proxmoxve/sdk/v6/go/proxmoxve/vm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Config structs
type ClusterConfig struct {
	NodeName    string
	DatastoreID string
	Bridge      string
	ImageUrl    string
	Nodes       []NodeConfig
}

type NodeConfig struct {
	Name     string
	Role     string
	Cores    int
	Memory   int
	DiskSize int
}

// initialize the Proxmox provider
func initializeProvider(ctx *pulumi.Context) (*proxmoxve.Provider, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	username := os.Getenv("PROXMOX_USERNAME")
	password := os.Getenv("PROXMOX_PASSWORD")
	endpoint := os.Getenv("PROXMOX_ENDPOINT")

	if username == "" || password == "" {
		return nil, fmt.Errorf("PROXMOX_USERNAME and PROXMOX_PASSWORD must be set in the .env file")
	}

	if endpoint == "" {
		endpoint = "https://192.168.1.198:8006/"
	}

	return proxmoxve.NewProvider(ctx, "proxmoxve", &proxmoxve.ProviderArgs{
		Endpoint: pulumi.String(endpoint),
		Username: pulumi.String(username),
		Password: pulumi.String(password),
		Insecure: pulumi.Bool(true),
	})
}

// creates a random MAC address
func generateMAC() (string, error) {
	mac := make([]byte, 6)
	if _, err := rand.Read(mac); err != nil {
		return "", err
	}
	mac[0] = (mac[0] | 0x02) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]), nil
}

// generates and uploads cloud-init config
func createCloudInit(ctx *pulumi.Context, provider *proxmoxve.Provider, nodeName, hostname, mac string) (*storage.File, error) {
	configPath := filepath.Join("cloud-init", "cloud-init.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	content := string(data)
	replacements := map[string]string{
		"${mac_address}": mac,
		"${hostname}":    hostname,
	}
	for k, v := range replacements {
		content = strings.ReplaceAll(content, k, v)
	}

	return storage.NewFile(ctx, hostname+"-cloud-init", &storage.FileArgs{
		NodeName:    pulumi.String(nodeName),
		DatastoreId: pulumi.String("local"),
		ContentType: pulumi.String("snippets"),
		FileMode:    pulumi.String("0755"),
		Overwrite:   pulumi.Bool(true),
		SourceRaw: storage.FileSourceRawArgs{
			Data:     pulumi.String(content),
			FileName: pulumi.String(hostname + ".yml"),
		},
	}, pulumi.Provider(provider))
}

// creates a VM
func createVM(ctx *pulumi.Context, provider *proxmoxve.Provider, config NodeConfig, cloudInit *storage.File, cloudImage *download.File, bridge string) (*vm.VirtualMachine, error) {
	return vm.NewVirtualMachine(ctx, config.Name, &vm.VirtualMachineArgs{
		NodeName: pulumi.String("pve"),
		Name:     pulumi.String(config.Name),
		Agent:    vm.VirtualMachineAgentArgs{Enabled: pulumi.Bool(true)},
		Cpu:      &vm.VirtualMachineCpuArgs{Cores: pulumi.Int(config.Cores)},
		Memory: &vm.VirtualMachineMemoryArgs{
			Dedicated: pulumi.Int(config.Memory),
			Floating:  pulumi.Int(config.Memory),
		},
		Disks: &vm.VirtualMachineDiskArray{
			vm.VirtualMachineDiskArgs{
				Size:       pulumi.Int(config.DiskSize),
				Interface:  pulumi.String("scsi0"),
				Iothread:   pulumi.Bool(true),
				FileFormat: pulumi.String("raw"),
				FileId:     cloudImage.ID(),
			},
		},
		BootOrders:   pulumi.StringArray{pulumi.String("scsi0"), pulumi.String("net0")},
		ScsiHardware: pulumi.String("virtio-scsi-single"),
		OperatingSystem: vm.VirtualMachineOperatingSystemArgs{
			Type: pulumi.String("l26"),
		},
		NetworkDevices: vm.VirtualMachineNetworkDeviceArray{
			vm.VirtualMachineNetworkDeviceArgs{
				Model:  pulumi.String("virtio"),
				Bridge: pulumi.String(bridge),
			},
		},
		OnBoot: pulumi.Bool(true),
		Initialization: &vm.VirtualMachineInitializationArgs{
			DatastoreId:    pulumi.String("local-lvm"),
			UserDataFileId: cloudInit.ID(),
		},
	}, pulumi.DependsOn([]pulumi.Resource{cloudImage}), pulumi.Provider(provider))
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		// Initialize provider
		provider, err := initializeProvider(ctx)
		if err != nil {
			return err
		}

		// Define cluster configs
		config := ClusterConfig{
			NodeName:    "pve",
			DatastoreID: "local",
			Bridge:      "vmbr0",
			ImageUrl:    "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			Nodes: []NodeConfig{
				{Name: "k8s-controller", Role: "control", Cores: 4, Memory: 8192, DiskSize: 50},
				{Name: "k8s-worker1", Role: "worker", Cores: 2, Memory: 4096, DiskSize: 40},
				{Name: "k8s-worker2", Role: "worker", Cores: 2, Memory: 4096, DiskSize: 40},
			},
		}

		// Download cloud image
		cloudImage, err := download.NewFile(ctx, "ubuntu-cloud-image", &download.FileArgs{
			ContentType: pulumi.String("iso"),
			DatastoreId: pulumi.String(config.DatastoreID),
			NodeName:    pulumi.String(config.NodeName),
			Url:         pulumi.String(config.ImageUrl),
		}, pulumi.Provider(provider))
		if err != nil {
			return err
		}

		for _, node := range config.Nodes {

			// Generate MAC addresses
			mac, err := generateMAC()
			if err != nil {
				return err
			}

			// Prepare cloud-init configs and upload
			cloudInit, err := createCloudInit(ctx, provider, config.NodeName, node.Name, mac)
			if err != nil {
				return err
			}

			// Create VMs
			vm, err := createVM(ctx, provider, node, cloudInit, cloudImage, config.Bridge)
			if err != nil {
				return err
			}

			ctx.Export(node.Name+"-id", vm.ID())
			ctx.Export(node.Name+"-ip", vm.Ipv4Addresses)
		}

		return nil
	})
}
