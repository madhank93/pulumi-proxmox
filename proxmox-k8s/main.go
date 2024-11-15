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
	"gopkg.in/yaml.v3"
)

// VMConfig holds the configuration for a VM
type VMConfig struct {
	Name          string
	NodeName      string
	Cores         int
	Memory        int
	DiskSize      int
	CloudInitPath *storage.File
	MacAddress    string
}

// ClusterConfig holds the configuration for the entire K8s cluster
type ClusterConfig struct {
	ControlPlane VMConfig
	Workers      []VMConfig
	ImageUrl     string
	NodeName     string
	DatastoreID  string
	Bridge       string
}

// FileUploadOptions contains the configurable options for file upload
type FileUploadOptions struct {
	NodeName    string
	DatastoreId string
	ContentType string
	FileMode    string
	Overwrite   bool
	Data        string
	FileName    string
}

// initializeProvider sets up the Proxmox provider
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

// downloadCloudImage downloads the Ubuntu cloud image
func downloadCloudImage(ctx *pulumi.Context, provider *proxmoxve.Provider, config ClusterConfig) (*download.File, error) {
	return download.NewFile(ctx, "download-image", &download.FileArgs{
		ContentType: pulumi.String("iso"),
		DatastoreId: pulumi.String(config.DatastoreID),
		NodeName:    pulumi.String(config.NodeName),
		Url:         pulumi.String(config.ImageUrl),
	}, pulumi.Provider(provider))
}

// generateRandomMAC generates a random MAC address
func generateRandomMAC() (string, error) {
	mac := make([]byte, 6)
	if _, err := rand.Read(mac); err != nil {
		return "", err
	}
	mac[0] = (mac[0] | 0x02) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]), nil
}

// UploadCloudConfig uploads a cloud-init config with appropriate defaults
func UploadCloudConfig(ctx *pulumi.Context, name string, provider *proxmoxve.Provider, config FileUploadOptions) (*storage.File, error) {
	return storage.NewFile(ctx, name, &storage.FileArgs{
		NodeName:    pulumi.String(config.NodeName),
		DatastoreId: pulumi.String(config.DatastoreId),
		ContentType: pulumi.String(config.ContentType),
		FileMode:    pulumi.String(config.FileMode),
		Overwrite:   pulumi.Bool(config.Overwrite),
		SourceRaw: storage.FileSourceRawArgs{
			Data:     pulumi.String(config.Data),
			FileName: pulumi.String(config.FileName),
		},
	}, pulumi.Provider(provider))
}

// readAndReplaceCloudInitConfig reads the cloud-init configuration file and replaces placeholders
func readAndReplaceCloudInitConfig(path string, replacements []map[string]string) (string, error) {
	// Read file content
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error reading cloud-init config: %v", err)
	}

	content := string(data)

	// Iterate through each map in replacements
	for _, replacement := range replacements {
		for key, value := range replacement {
			content = strings.ReplaceAll(content, "${"+key+"}", value)
		}
	}

	// Validate the resulting YAML
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return "", fmt.Errorf("invalid cloud-init yaml after replacements: %v", err)
	}

	return content, nil
}

// createVM creates a single VM with the given configuration
func createVM(ctx *pulumi.Context, provider *proxmoxve.Provider, vmConfig VMConfig, cloudImage *download.File, bridge string) (*vm.VirtualMachine, error) {
	return vm.NewVirtualMachine(ctx, vmConfig.Name, &vm.VirtualMachineArgs{
		NodeName: pulumi.String(vmConfig.NodeName),
		Name:     pulumi.String(vmConfig.Name),
		Agent:    vm.VirtualMachineAgentArgs{Enabled: pulumi.Bool(true)},
		Cpu:      &vm.VirtualMachineCpuArgs{Cores: pulumi.Int(vmConfig.Cores)},
		Memory: &vm.VirtualMachineMemoryArgs{
			Dedicated: pulumi.Int(vmConfig.Memory),
			Floating:  pulumi.Int(vmConfig.Memory),
		},
		Disks: &vm.VirtualMachineDiskArray{
			vm.VirtualMachineDiskArgs{
				Size:       pulumi.Int(vmConfig.DiskSize),
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
				Model:      pulumi.String("virtio"),
				Bridge:     pulumi.String(bridge),
				MacAddress: pulumi.String(vmConfig.MacAddress),
			},
		},
		OnBoot: pulumi.Bool(true),
		Initialization: &vm.VirtualMachineInitializationArgs{
			DatastoreId:    pulumi.String("local-lvm"),
			UserDataFileId: vmConfig.CloudInitPath.ID(),
		},
	}, pulumi.DependsOn([]pulumi.Resource{cloudImage}), pulumi.Provider(provider))
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Initialize provider
		provider, err := initializeProvider(ctx)
		if err != nil {
			return fmt.Errorf("error initializing the provider: %v", err)
		}

		// Generate MAC addresses
		controlPlaneMAC, err := generateRandomMAC()
		if err != nil {
			return fmt.Errorf("failed to generate MAC for control plane: %v", err)
		}
		worker1MAC, err := generateRandomMAC()
		if err != nil {
			return fmt.Errorf("failed to generate MAC for worker 1: %v", err)
		}
		worker2MAC, err := generateRandomMAC()
		if err != nil {
			return fmt.Errorf("failed to generate MAC for worker 2: %v", err)
		}

		// Prepare cloud-init configs
		workDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("error getting working directory: %v", err)
		}

		controlPlaneCloudInitData, err := readAndReplaceCloudInitConfig(
			filepath.Join(workDir, "cloud-init", "cloud-init.yml"),
			[]map[string]string{
				{"mac_address": controlPlaneMAC},
				{"hostname": "controller"},
			},
		)
		if err != nil {
			return fmt.Errorf("error preparing control plane cloud config: %v", err)
		}

		worker1CloudInitData, err := readAndReplaceCloudInitConfig(
			filepath.Join(workDir, "cloud-init", "cloud-init.yml"),
			[]map[string]string{
				{"mac_address": worker1MAC},
				{"hostname": "worker1"},
			},
		)
		if err != nil {
			return fmt.Errorf("error preparing worker 1 cloud config: %v", err)
		}

		worker2CloudInitData, err := readAndReplaceCloudInitConfig(
			filepath.Join(workDir, "cloud-init", "cloud-init.yml"),
			[]map[string]string{
				{"mac_address": worker2MAC},
				{"hostname": "worker2"},
			},
		)
		if err != nil {
			return fmt.Errorf("error preparing worker 2 cloud config: %v", err)
		}

		// Upload cloud-init configs
		controlPlaneCloudInit, err := UploadCloudConfig(ctx, "cloud-init", provider, FileUploadOptions{
			NodeName:    "pve",
			DatastoreId: "local",
			ContentType: "snippets",
			FileMode:    "0755",
			Overwrite:   true,
			Data:        controlPlaneCloudInitData,
			FileName:    "control-plane.yml",
		})
		if err != nil {
			return fmt.Errorf("error uploading control-plane cloud config: %v", err)
		}

		worker1CloudInit, err := UploadCloudConfig(ctx, "worker1-config", provider, FileUploadOptions{
			NodeName:    "pve",
			DatastoreId: "local",
			ContentType: "snippets",
			FileMode:    "0755",
			Overwrite:   true,
			Data:        worker1CloudInitData,
			FileName:    "worker1.yml",
		})
		if err != nil {
			return fmt.Errorf("error uploading worker1 cloud config: %v", err)
		}

		worker2CloudInit, err := UploadCloudConfig(ctx, "worker2-config", provider, FileUploadOptions{
			NodeName:    "pve",
			DatastoreId: "local",
			ContentType: "snippets",
			FileMode:    "0755",
			Overwrite:   true,
			Data:        worker2CloudInitData,
			FileName:    "worker2.yml",
		})
		if err != nil {
			return fmt.Errorf("error uploading worker2 cloud config: %v", err)
		}

		// Define cluster configuration
		clusterConfig := ClusterConfig{
			ControlPlane: VMConfig{
				Name:          "k8s-control-plane",
				NodeName:      "pve",
				Cores:         4,
				Memory:        8192,
				DiskSize:      50,
				CloudInitPath: controlPlaneCloudInit,
				MacAddress:    controlPlaneMAC,
			},
			Workers: []VMConfig{
				{
					Name:          "k8s-worker-1",
					NodeName:      "pve",
					Cores:         2,
					Memory:        4096,
					DiskSize:      40,
					CloudInitPath: worker1CloudInit,
					MacAddress:    worker1MAC,
				},
				{
					Name:          "k8s-worker-2",
					NodeName:      "pve",
					Cores:         2,
					Memory:        4096,
					DiskSize:      40,
					CloudInitPath: worker2CloudInit,
					MacAddress:    worker2MAC,
				},
			},
			ImageUrl:    "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			NodeName:    "pve",
			DatastoreID: "local",
			Bridge:      "vmbr0",
		}

		// Download cloud image
		cloudImage, err := downloadCloudImage(ctx, provider, clusterConfig)
		if err != nil {
			return err
		}

		// Create control plane node
		controlPlane, err := createVM(ctx, provider, clusterConfig.ControlPlane, cloudImage, clusterConfig.Bridge)
		if err != nil {
			return err
		}
		ctx.Export("controlNodeId", controlPlane.ID())

		// Create worker nodes
		for i, workerConfig := range clusterConfig.Workers {
			worker, err := createVM(ctx, provider, workerConfig, cloudImage, clusterConfig.Bridge)
			if err != nil {
				return err
			}
			ctx.Export(fmt.Sprintf("workerNode%dId", i+1), worker.ID())
		}

		return nil
	})
}
