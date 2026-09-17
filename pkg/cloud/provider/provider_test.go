package provider

import (
	"testing"

	"github.com/opencost/opencost/core/pkg/clustercache"
	coreenv "github.com/opencost/opencost/core/pkg/env"
	"github.com/opencost/opencost/core/pkg/storage"
	"github.com/opencost/opencost/pkg/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestParseLocalDiskID(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"empty string": {
			input: "",
			want:  "",
		},
		"generic string": {
			input: "test",
			want:  "test",
		},
		"AWS node provider id": {
			input: "aws:///us-east-2a/i-0fea4fd46592d050b",
			want:  "i-0fea4fd46592d050b",
		},
		"GCP node provider id": {
			input: "gce://guestbook-11111/us-central1-a/gke-niko-n1-standard-2-wlkla-8d48e58a-hfy7",
			want:  "gke-niko-n1-standard-2-wlkla-8d48e58a-hfy7",
		},
		"Azure vmss provider id": {
			input: "azure:///subscriptions/ae337b64-e7ba-3387-b043-187289efe4e3/resourceGroups/mc_test_eastus2/providers/Microsoft.Compute/virtualMachineScaleSets/aks-userpool-12345678-vmss/virtualMachines/11",
			want:  "azure:///subscriptions/ae337b64-e7ba-3387-b043-187289efe4e3/resourcegroups/mc_test_eastus2/providers/microsoft.compute/disks/aks-userpool-12345678-vmss00000b_osdisk",
		},
		"Azure vm provider id": {
			input: "azure:///subscriptions/ae337b64-e7ba-3387-b043-187289efe4e3/resourceGroups/mc_test_eastus2/providers/Microsoft.Compute/virtualMachines/master-0",
			want:  "azure:///subscriptions/ae337b64-e7ba-3387-b043-187289efe4e3/resourcegroups/mc_test_eastus2/providers/microsoft.compute/disks/master-0_osdisk",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ParseLocalDiskID(tt.input); got != tt.want {
				t.Errorf("ParseLocalDiskID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderConfigUpdateFromMapPreservesHourlyPrices(t *testing.T) {
	confMan := config.NewConfigFileManager(storage.NewMemoryStorage())
	providerConfig := NewProviderConfig(confMan, "default.json")

	updated, err := providerConfig.UpdateFromMap(map[string]string{
		"CPU":     "0.031611",
		"spotCPU": "0.006655",
		"RAM":     "0.004237",
		"spotRAM": "0.000892",
		"GPU":     "0.95",
		"spotGPU": "0.308",
		"storage": "0.00005479452",
	})
	if err != nil {
		t.Fatalf("UpdateFromMap returned error: %v", err)
	}

	if updated.CPU != "0.031611" {
		t.Errorf("CPU = %q, want hourly value %q", updated.CPU, "0.031611")
	}
	if updated.SpotCPU != "0.006655" {
		t.Errorf("SpotCPU = %q, want hourly value %q", updated.SpotCPU, "0.006655")
	}
	if updated.RAM != "0.004237" {
		t.Errorf("RAM = %q, want hourly value %q", updated.RAM, "0.004237")
	}
	if updated.SpotRAM != "0.000892" {
		t.Errorf("SpotRAM = %q, want hourly value %q", updated.SpotRAM, "0.000892")
	}
	if updated.GPU != "0.95" {
		t.Errorf("GPU = %q, want hourly value %q", updated.GPU, "0.95")
	}
	if updated.SpotGPU != "0.308" {
		t.Errorf("SpotGPU = %q, want hourly value %q", updated.SpotGPU, "0.308")
	}
	if updated.Storage != "0.00005479452" {
		t.Errorf("Storage = %q, want hourly value %q", updated.Storage, "0.00005479452")
	}
}

func TestCustomProviderGetKeyDetectsGPUCapacity(t *testing.T) {
	cases := []struct {
		name         string
		provider     *CustomProvider
		labels       map[string]string
		capacity     v1.ResourceList
		wantGPUType  string
		wantGPUCount int
	}{
		{
			name: "nvidia GPU capacity",
			capacity: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("2"),
			},
			wantGPUType:  "nvidia.com/gpu",
			wantGPUCount: 2,
		},
		{
			name: "virtual GPU capacity",
			capacity: v1.ResourceList{
				"k8s.amazonaws.com/vgpu": resource.MustParse("3"),
			},
			wantGPUType:  "k8s.amazonaws.com/vgpu",
			wantGPUCount: 3,
		},
		{
			name: "configured GPU label takes precedence over capacity type",
			provider: &CustomProvider{
				GPULabel: "gpu.example/type",
			},
			labels: map[string]string{
				"gpu.example/type": "a100",
			},
			capacity: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("4"),
			},
			wantGPUType:  "a100",
			wantGPUCount: 4,
		},
		{
			name:         "no GPU capacity",
			capacity:     v1.ResourceList{},
			wantGPUType:  "",
			wantGPUCount: 0,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			customProvider := tt.provider
			if customProvider == nil {
				customProvider = &CustomProvider{}
			}
			labels := tt.labels
			if labels == nil {
				labels = map[string]string{}
			}

			key := customProvider.GetKey(labels, &clustercache.Node{
				Labels: labels,
				Status: v1.NodeStatus{
					Capacity: tt.capacity,
				},
			})

			if got := key.GPUType(); got != tt.wantGPUType {
				t.Errorf("GPUType() = %q, want %q", got, tt.wantGPUType)
			}
			if got := key.GPUCount(); got != tt.wantGPUCount {
				t.Errorf("GPUCount() = %d, want %d", got, tt.wantGPUCount)
			}
		})
	}
}

func TestCustomProviderNodePricingUsesDetectedGPUCount(t *testing.T) {
	customProvider := &CustomProvider{
		Pricing: map[string]*NodePrice{
			"default": {
				CPU: "0.031611",
				RAM: "0.004237",
			},
			"default,gpu": {
				CPU: "0.031611",
				RAM: "0.004237",
				GPU: "0.95",
			},
		},
	}

	key := customProvider.GetKey(map[string]string{}, &clustercache.Node{
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("2"),
			},
		},
	})

	node, _, err := customProvider.NodePricing(key)
	if err != nil {
		t.Fatalf("NodePricing returned error: %v", err)
	}

	if node.VCPUCost != "0.031611" {
		t.Errorf("VCPUCost = %q, want %q", node.VCPUCost, "0.031611")
	}
	if node.RAMCost != "0.004237" {
		t.Errorf("RAMCost = %q, want %q", node.RAMCost, "0.004237")
	}
	if node.GPUCost != "0.95" {
		t.Errorf("GPUCost = %q, want %q", node.GPUCost, "0.95")
	}
	if node.GPU != "2" {
		t.Errorf("GPU = %q, want %q", node.GPU, "2")
	}
}

func TestCustomProviderClusterInfoUsesStaticDefaultName(t *testing.T) {
	t.Setenv(coreenv.ClusterIDEnvVar, "")

	customProvider := newTestCustomProvider(t, nil)

	info, err := customProvider.ClusterInfo()
	if err != nil {
		t.Fatalf("ClusterInfo returned error: %v", err)
	}

	if info["name"] != "Custom Cluster" {
		t.Errorf("name = %q, want %q", info["name"], "Custom Cluster")
	}
	if info["id"] != "default-cluster" {
		t.Errorf("id = %q, want %q", info["id"], "default-cluster")
	}
}

func TestCustomProviderLoadBalancerPricingEmptyConfig(t *testing.T) {
	customProvider := newTestCustomProvider(t, nil)

	lb, err := customProvider.LoadBalancerPricing()
	if err != nil {
		t.Fatalf("LoadBalancerPricing returned error: %v", err)
	}
	if lb.Cost != 0 {
		t.Errorf("Cost = %f, want 0", lb.Cost)
	}
}

func TestCustomProviderLoadBalancerPricingUsesDefaultLBPriceFallback(t *testing.T) {
	customProvider := newTestCustomProvider(t, map[string]string{
		"defaultLBPrice": "0.025",
	})

	lb, err := customProvider.LoadBalancerPricing()
	if err != nil {
		t.Fatalf("LoadBalancerPricing returned error: %v", err)
	}
	if lb.Cost != 0.025 {
		t.Errorf("Cost = %f, want 0.025", lb.Cost)
	}
}

func TestCustomProviderLoadBalancerPricingUsesForwardingRulePrice(t *testing.T) {
	customProvider := newTestCustomProvider(t, map[string]string{
		"firstFiveForwardingRulesCost": "0.02",
		"defaultLBPrice":               "0.025",
	})

	lb, err := customProvider.LoadBalancerPricing()
	if err != nil {
		t.Fatalf("LoadBalancerPricing returned error: %v", err)
	}
	if lb.Cost != 0.02 {
		t.Errorf("Cost = %f, want 0.02", lb.Cost)
	}
}

func TestCustomProviderLoadBalancerPricingInvalidValue(t *testing.T) {
	customProvider := newTestCustomProvider(t, map[string]string{
		"firstFiveForwardingRulesCost": "not-a-price",
	})

	_, err := customProvider.LoadBalancerPricing()
	if err == nil {
		t.Fatal("LoadBalancerPricing returned nil error, want invalid pricing error")
	}
}

func newTestCustomProvider(t *testing.T, pricing map[string]string) *CustomProvider {
	t.Helper()

	confMan := config.NewConfigFileManager(storage.NewMemoryStorage())
	providerConfig := NewProviderConfig(confMan, "default.json")
	if pricing != nil {
		if _, err := providerConfig.UpdateFromMap(pricing); err != nil {
			t.Fatalf("UpdateFromMap returned error: %v", err)
		}
	}

	return &CustomProvider{
		Config: providerConfig,
	}
}

func TestCustomProviderPricingStableAcrossUpdates(t *testing.T) {
	confMan := config.NewConfigFileManager(storage.NewMemoryStorage())
	customProvider := &CustomProvider{
		Config: NewProviderConfig(confMan, "default.json"),
	}

	pricing := map[string]string{
		"CPU":     "0.006407",
		"RAM":     "0.000859",
		"spotCPU": "0.006655",
		"spotRAM": "0.000892",
		"GPU":     "0.95",
		"storage": "0.00005479452",
	}

	if _, err := customProvider.UpdateConfigFromConfigMap(pricing); err != nil {
		t.Fatalf("UpdateConfigFromConfigMap returned error: %v", err)
	}

	// Repeated reads of the config must return the same configured values.
	for i := 0; i < 5; i++ {
		conf, err := customProvider.GetConfig()
		if err != nil {
			t.Fatalf("GetConfig returned error on iteration %d: %v", i, err)
		}

		if conf.CPU != pricing["CPU"] {
			t.Errorf("iteration %d: CPU = %q, want %q", i, conf.CPU, pricing["CPU"])
		}
		if conf.RAM != pricing["RAM"] {
			t.Errorf("iteration %d: RAM = %q, want %q", i, conf.RAM, pricing["RAM"])
		}
		if conf.SpotCPU != pricing["spotCPU"] {
			t.Errorf("iteration %d: SpotCPU = %q, want %q", i, conf.SpotCPU, pricing["spotCPU"])
		}
		if conf.SpotRAM != pricing["spotRAM"] {
			t.Errorf("iteration %d: SpotRAM = %q, want %q", i, conf.SpotRAM, pricing["spotRAM"])
		}
		if conf.GPU != pricing["GPU"] {
			t.Errorf("iteration %d: GPU = %q, want %q", i, conf.GPU, pricing["GPU"])
		}
		if conf.Storage != pricing["storage"] {
			t.Errorf("iteration %d: Storage = %q, want %q", i, conf.Storage, pricing["storage"])
		}
	}

	// Updating with the same values again must not divide or mutate them.
	if _, err := customProvider.UpdateConfigFromConfigMap(pricing); err != nil {
		t.Fatalf("UpdateConfigFromConfigMap (second update) returned error: %v", err)
	}

	conf, err := customProvider.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig returned error after second update: %v", err)
	}
	if conf.CPU != pricing["CPU"] {
		t.Errorf("CPU = %q, want %q after second update", conf.CPU, pricing["CPU"])
	}
	if conf.RAM != pricing["RAM"] {
		t.Errorf("RAM = %q, want %q after second update", conf.RAM, pricing["RAM"])
	}
	if conf.SpotCPU != pricing["spotCPU"] {
		t.Errorf("SpotCPU = %q, want %q after second update", conf.SpotCPU, pricing["spotCPU"])
	}
	if conf.SpotRAM != pricing["spotRAM"] {
		t.Errorf("SpotRAM = %q, want %q after second update", conf.SpotRAM, pricing["spotRAM"])
	}
	if conf.GPU != pricing["GPU"] {
		t.Errorf("GPU = %q, want %q after second update", conf.GPU, pricing["GPU"])
	}
	if conf.Storage != pricing["storage"] {
		t.Errorf("Storage = %q, want %q after second update", conf.Storage, pricing["storage"])
	}

	// Node pricing for an unknown key must fall back to the configured
	// default pricing, not a divided value.
	if err := customProvider.DownloadPricingData(); err != nil {
		t.Fatalf("DownloadPricingData returned error: %v", err)
	}

	node, _, err := customProvider.NodePricing(unknownCustomPricingKey{})
	if err != nil {
		t.Fatalf("NodePricing returned error: %v", err)
	}
	if node.VCPUCost != pricing["CPU"] {
		t.Errorf("VCPUCost = %q, want configured value %q", node.VCPUCost, pricing["CPU"])
	}
	if node.RAMCost != pricing["RAM"] {
		t.Errorf("RAMCost = %q, want configured value %q", node.RAMCost, pricing["RAM"])
	}
}

// unknownCustomPricingKey is a models.Key that matches no entry in the
// CustomProvider pricing map, forcing the fallback to the default pricing.
type unknownCustomPricingKey struct{}

func (unknownCustomPricingKey) ID() string       { return "unknown-node" }
func (unknownCustomPricingKey) Features() string { return "unknown-feature" }
func (unknownCustomPricingKey) GPUType() string  { return "" }
func (unknownCustomPricingKey) GPUCount() int    { return 0 }
