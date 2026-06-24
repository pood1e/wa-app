package app

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// device_profiles.json 是内嵌的默认注册设备画像池。运行时可用环境变量
// WA_APP_DEVICE_PROFILES_FILE 指向外部文件覆盖(挂 ConfigMap 即可后续增删机型,
// 无需重建镜像)。文件非法/为空时保留当前池并记录,绝不让注册因配置问题中断。
//
//go:embed device_profiles.json
var defaultDeviceProfilesJSON []byte

type deviceProfileEntry struct {
	Vendor         string  `json:"vendor"`
	Model          string  `json:"model"`
	Android        string  `json:"android"`
	BuildDisplayID string  `json:"build_display_id"`
	MinRAMGiB      float64 `json:"min_ram_gib"`
	MaxRAMGiB      float64 `json:"max_ram_gib"`
}

type deviceProfilesDocument struct {
	RegistrationProfiles []deviceProfileEntry `json:"registration_profiles"`
}

var (
	registrationDeviceModelsMu  sync.RWMutex
	registrationDeviceModelPool = parseDefaultDeviceProfiles()
)

// parseDefaultDeviceProfiles 解析内嵌默认池;即使内嵌 JSON 损坏也回退到单台已校验真机,
// 保证注册始终有可用画像。
func parseDefaultDeviceProfiles() []nativeDeviceModel {
	if models, err := parseDeviceProfiles(defaultDeviceProfilesJSON); err == nil && len(models) > 0 {
		return models
	}
	return []nativeDeviceModel{{Vendor: "Google", Model: "Pixel 9 Pro XL", Android: "16", BuildDisplayID: "CP1A.260505.005", MinRAMGiB: 15.22, MaxRAMGiB: 15.22}}
}

// LoadRegistrationDeviceProfiles 用外部配置文件覆盖注册设备画像池;路径为空则保持内置默认。
func LoadRegistrationDeviceProfiles(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("WA device profiles: read %s failed, keeping built-in pool: %v", path, sanitizeLogError(err))
		return err
	}
	models, err := parseDeviceProfiles(data)
	if err != nil {
		log.Printf("WA device profiles: parse %s failed, keeping built-in pool: %v", path, sanitizeLogError(err))
		return err
	}
	if len(models) == 0 {
		log.Printf("WA device profiles: %s has no valid profiles, keeping built-in pool", path)
		return fmt.Errorf("no valid device profiles in %s", path)
	}
	registrationDeviceModelsMu.Lock()
	registrationDeviceModelPool = models
	registrationDeviceModelsMu.Unlock()
	log.Printf("WA device profiles: loaded %d registration profiles from %s", len(models), path)
	return nil
}

func parseDeviceProfiles(data []byte) ([]nativeDeviceModel, error) {
	var doc deviceProfilesDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	models := make([]nativeDeviceModel, 0, len(doc.RegistrationProfiles))
	for _, entry := range doc.RegistrationProfiles {
		if model, ok := entry.toNativeDeviceModel(); ok {
			models = append(models, model)
		}
	}
	return models, nil
}

func (e deviceProfileEntry) toNativeDeviceModel() (nativeDeviceModel, bool) {
	vendor := strings.TrimSpace(e.Vendor)
	model := strings.TrimSpace(e.Model)
	android := strings.TrimSpace(e.Android)
	build := strings.TrimSpace(e.BuildDisplayID)
	if vendor == "" || model == "" || android == "" || build == "" || e.MinRAMGiB <= 0 {
		return nativeDeviceModel{}, false
	}
	maxRAM := e.MaxRAMGiB
	if maxRAM < e.MinRAMGiB {
		maxRAM = e.MinRAMGiB
	}
	return nativeDeviceModel{Vendor: vendor, Model: model, Android: android, BuildDisplayID: build, MinRAMGiB: e.MinRAMGiB, MaxRAMGiB: maxRAM}, true
}

func registrationDeviceModels() []nativeDeviceModel {
	registrationDeviceModelsMu.RLock()
	defer registrationDeviceModelsMu.RUnlock()
	return registrationDeviceModelPool
}
