package antifingerprint

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	srand "wechatdll/clientsdk/securerandom"
)

type DeviceProfile struct {
	Manufacturer string
	Brand        string
	Model        string
	Device       string
	Product      string
	Hardware     string
	Board        string
	BuildFP      string
	OsVersion    string
	ApiLevel     int
	ScreenW      int
	ScreenH      int
	Dpi          int
}

var (
	ipadProfiles = []DeviceProfile{
		{Model: "iPad14,4", Device: "J507AP", Product: "J507AP", OsVersion: "18.8.1", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8101", Board: "t8101", BuildFP: "Apple/iPad14,4/18.8.1/22A87"},
		{Model: "iPad14,5", Device: "J508AP", Product: "J508AP", OsVersion: "18.8.1", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8101", Board: "t8101", BuildFP: "Apple/iPad14,5/18.8.1/22A87"},
		{Model: "iPad14,6", Device: "J509AP", Product: "J509AP", OsVersion: "18.8.1", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8122", Board: "t8122", BuildFP: "Apple/iPad14,6/18.8.1/22A87"},
		{Model: "iPad14,7", Device: "J510AP", Product: "J510AP", OsVersion: "18.8.1", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8122", Board: "t8122", BuildFP: "Apple/iPad14,7/18.8.1/22A87"},
		{Model: "iPad13,18", Device: "J491AP", Product: "J491AP", OsVersion: "18.5", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8101", Board: "t8101", BuildFP: "Apple/iPad13,18/18.5/22G90"},
		{Model: "iPad13,19", Device: "J492AP", Product: "J492AP", OsVersion: "18.5", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8101", Board: "t8101", BuildFP: "Apple/iPad13,19/18.5/22G90"},
		{Model: "iPad13,16", Device: "J481AP", Product: "J481AP", OsVersion: "18.5", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8112", Board: "t8112", BuildFP: "Apple/iPad13,16/18.5/22G90"},
		{Model: "iPad13,17", Device: "J482AP", Product: "J482AP", OsVersion: "18.5", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8112", Board: "t8112", BuildFP: "Apple/iPad13,17/18.5/22G90"},
		{Model: "iPad16,1", Device: "J620AP", Product: "J620AP", OsVersion: "18.4", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8140", Board: "t8140", BuildFP: "Apple/iPad16,1/18.4/22E778"},
		{Model: "iPad16,2", Device: "J621AP", Product: "J621AP", OsVersion: "18.4", Manufacturer: "Apple", Brand: "Apple", Hardware: "t8140", Board: "t8140", BuildFP: "Apple/iPad16,2/18.4/22E778"},
	}

	androidProfiles = []DeviceProfile{
		{Manufacturer: "HUAWEI", Brand: "HUAWEI", Model: "GRL-AL10", Device: "GRL", Product: "GRL-AL10", Hardware: "qcom", Board: "k1v1", OsVersion: "12", ApiLevel: 31, BuildFP: "HUAWEI/GRL-AL10/HWGRL:12/HUAWEIGRL-AL10/103.0.0.230:user/release-keys", ScreenW: 1080, ScreenH: 2220, Dpi: 480},
		{Manufacturer: "HUAWEI", Brand: "HUAWEI", Model: "TYY-AL00", Device: "TYY", Product: "TYY-AL00", Hardware: "qcom", Board: "k1v1", OsVersion: "12", ApiLevel: 31, BuildFP: "HUAWEI/TYY-AL00/HWTYY:12/HUAWEITYY-AL00/103.0.0.230:user/release-keys", ScreenW: 1080, ScreenH: 2340, Dpi: 480},
		{Manufacturer: "Xiaomi", Brand: "Xiaomi", Model: "23116PN5BC", Device: "nuwa", Product: "23116PN5BC", Hardware: "qcom", Board: "taro", OsVersion: "14", ApiLevel: 34, BuildFP: "Xiaomi/23116PN5BC/nuwa:14/UQ1A.240205.004/V816.0.6.0.UMACNXM:user/release-keys", ScreenW: 1080, ScreenH: 2400, Dpi: 440},
		{Manufacturer: "Xiaomi", Brand: "Redmi", Model: "2312CRA74C", Device: "air", Product: "2312CRA74C", Hardware: "mt6789", Board: "t8166_tb", OsVersion: "14", ApiLevel: 34, BuildFP: "Redmi/2312CRA74C/air:14/UQ1A.240205.004/V816.0.4.0.ULACNXM:user/release-keys", ScreenW: 1080, ScreenH: 2400, Dpi: 440},
		{Manufacturer: "samsung", Brand: "samsung", Model: "SM-S928B", Device: "e3q", Product: "e3qxxx", Hardware: "qcom", Board: "e3q", OsVersion: "14", ApiLevel: 34, BuildFP: "samsung/e3qxxx/e3q:14/UQ1A.240205.004/S928BXXU3AXK1:user/release-keys", ScreenW: 1440, ScreenH: 3120, Dpi: 560},
		{Manufacturer: "OPPO", Brand: "OPPO", Model: "PHZ110", Device: "OP592DL1", Product: "PHZ110", Hardware: "qcom", Board: "taro", OsVersion: "14", ApiLevel: 34, BuildFP: "OPPO/PHZ110/OP592DL1:14/UQ1A.240205.004/PHZ110_14.1.0.120_CN:user/release-keys", ScreenW: 1080, ScreenH: 2412, Dpi: 480},
		{Manufacturer: "vivo", Brand: "vivo", Model: "V2331A", Device: "V2331A", Product: "V2331A", Hardware: "qcom", Board: "taro", OsVersion: "14", ApiLevel: 34, BuildFP: "vivo/V2331A/V2331A:14/UQ1A.240205.004/compiler03061149:user/release-keys", ScreenW: 1080, ScreenH: 2400, Dpi: 480},
	}

	androidPadProfiles = []DeviceProfile{
		{Manufacturer: "Xiaomi", Brand: "Xiaomi", Model: "2410CRP4C", Device: "dizi", Product: "2410CRP4C", Hardware: "qcom", Board: "parrot", OsVersion: "15", ApiLevel: 35, BuildFP: "Xiaomi/2410CRP4C/dizi:15/AP3A.241005.015/OS2.0.5.0.VOZCNXM:user/release-keys", ScreenW: 1600, ScreenH: 2560, Dpi: 320},
		{Manufacturer: "HUAWEI", Brand: "HUAWEI", Model: "MRO-W00", Device: "MRO", Product: "MRO-W00", Hardware: "qcom", Board: "k1v1", OsVersion: "12", ApiLevel: 31, BuildFP: "HUAWEI/MRO-W00/HWMRO:12/HUAWEIMRO-W00/103.0.0.230:user/release-keys", ScreenW: 1600, ScreenH: 2560, Dpi: 320},
		{Manufacturer: "samsung", Brand: "samsung", Model: "SM-X910", Device: "gts10lwifi", Product: "gts10lwifixx", Hardware: "qcom", Board: "p3s", OsVersion: "14", ApiLevel: 34, BuildFP: "samsung/gts10lwifixx/gts10lwifi:14/UQ1A.240205.004/X910XXU4CXK1:user/release-keys", ScreenW: 1600, ScreenH: 2560, Dpi: 320},
	}

	profileCache   = make(map[string]*DeviceProfile)
	profileCacheMu sync.RWMutex
)

func GetIPadProfile(deviceId string) *DeviceProfile {
	profileCacheMu.RLock()
	if p, ok := profileCache["ipad_"+deviceId]; ok {
		profileCacheMu.RUnlock()
		return p
	}
	profileCacheMu.RUnlock()

	idx := srand.SecureIntn(len(ipadProfiles))
	p := ipadProfiles[idx]

	profileCacheMu.Lock()
	profileCache["ipad_"+deviceId] = &p
	profileCacheMu.Unlock()
	return &p
}

func GetAndroidProfile(deviceId string) *DeviceProfile {
	profileCacheMu.RLock()
	if p, ok := profileCache["android_"+deviceId]; ok {
		profileCacheMu.RUnlock()
		return p
	}
	profileCacheMu.RUnlock()

	idx := srand.SecureIntn(len(androidProfiles))
	p := androidProfiles[idx]

	profileCacheMu.Lock()
	profileCache["android_"+deviceId] = &p
	profileCacheMu.Unlock()
	return &p
}

func GetAndroidPadProfile(deviceId string) *DeviceProfile {
	profileCacheMu.RLock()
	if p, ok := profileCache["androidpad_"+deviceId]; ok {
		profileCacheMu.RUnlock()
		return p
	}
	profileCacheMu.RUnlock()

	idx := srand.SecureIntn(len(androidPadProfiles))
	p := androidPadProfiles[idx]

	profileCacheMu.Lock()
	profileCache["androidpad_"+deviceId] = &p
	profileCacheMu.Unlock()
	return &p
}

func GenerateInstallTime() uint64 {
	now := uint64(time.Now().Unix())
	daysAgo := srand.SecureIntn(180) + 30
	offset := uint64(daysAgo * 86400)
	hourOffset := uint64(srand.SecureIntn(86400))
	installTime := now - offset + hourOffset
	return installTime
}

func GenerateKernBootTime() uint64 {
	now := uint64(time.Now().Unix())
	daysAgo := srand.SecureIntn(365) + 180
	offset := uint64(daysAgo * 86400)
	hourOffset := uint64(srand.SecureIntn(86400))
	return now - offset + hourOffset
}

func GenerateStatTime() (atime, mtime, ctime, btime uint64) {
	now := uint64(time.Now().Unix())
	daysAgo := srand.SecureIntn(30) + 1
	offset := uint64(daysAgo * 86400)
	base := now - offset

	atime = base + uint64(srand.SecureIntn(86400))
	mtime = base - uint64(srand.SecureIntn(3600))
	ctime = mtime - uint64(srand.SecureIntn(1800))
	btime = ctime - uint64(srand.SecureIntn(86400)*30+86400)
	return
}

func GenerateCarrier() string {
	carriers := []string{"中国联通", "中国移动", "中国电信", "中国广电"}
	return carriers[srand.SecureIntn(len(carriers))]
}

func GenerateNetworkInterface() string {
	ifaces := []string{"en0", "en1", "en2"}
	return ifaces[srand.SecureIntn(len(ifaces))]
}

func GenerateBatteryLevel() int32 {
	return int32(srand.SecureIntn(60) + 40)
}

func GenerateStorageInfo() (total, free uint64) {
	total = uint64(srand.SecureIntn(128)+64) * 1024 * 1024 * 1024
	free = uint64(srand.SecureIntn(32)+8) * 1024 * 1024 * 1024
	return
}

func GenerateDocumentDirUUID() string {
	b := srand.SecureRandBytes(16)
	return fmt.Sprintf("%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

func GenerateAppUUID() string {
	b := srand.SecureRandBytes(16)
	return fmt.Sprintf("%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

func GenerateFileUUID() string {
	b := srand.SecureRandBytes(16)
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func GenerateAndroidBuildFP(manufacturer, model, osVersion, deviceId string) string {
	buildId := generateAndroidBuildID(deviceId)
	incremental := generateAndroidIncremental(deviceId)
	return fmt.Sprintf("%s/%s/%s:%s/%s/%s:user/release-keys",
		manufacturer, model, strings.ToLower(model),
		osVersion, buildId, incremental)
}

func generateAndroidBuildID(deviceId string) string {
	s := srand.SecureRandBytes(4)
	num := srand.SecureIntn(9000) + 1000
	return fmt.Sprintf("TP1A.%d.%02X%02X", num, s[0], s[1])
}

func generateAndroidIncremental(deviceId string) string {
	num := srand.SecureIntn(9000000) + 1000000
	return strconv.Itoa(num)
}

var androidBuildFPMap = make(map[string]string)
var androidBuildFPMu sync.Mutex

func GetConsistentBuildFP(deviceId string, profile *DeviceProfile) string {
	androidBuildFPMu.Lock()
	defer androidBuildFPMu.Unlock()

	if fp, ok := androidBuildFPMap[deviceId]; ok {
		return fp
	}

	fp := profile.BuildFP
	androidBuildFPMap[deviceId] = fp
	return fp
}
