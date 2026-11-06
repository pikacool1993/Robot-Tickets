package v08

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"runtime"
	"sync"
	"wechatdll/clientsdk/dynlib"

	"github.com/lunny/log"
)

var (
	lib      dynlib.DynamicLibrary
	mu       sync.Mutex
	isInit   bool = false
	initOnce sync.Once
	callMu   sync.Mutex
)

func init() {
	fmt.Printf("init v08: %v\n", runtime.GOOS)
	var libPath string
	switch runtime.GOOS {
	case "windows":
		libPath = "lib\\v08.dll"
	case "linux":
		libPath = "lib/libv08.so"
	case "darwin":
		libPath = "lib/libv08.dylib"
	default:
		panic("unsupported platform")
	}
	vip08()
	var err error
	lib, err = dynlib.NewLibrary(libPath)
	if err != nil {
		log.Error("Failed to load library: ", err)
	}
}

func ensureInit() {
	initOnce.Do(func() {
		mu.Lock()
		defer mu.Unlock()
		callMu.Lock()
		defer callMu.Unlock()
		if lib != nil {
			lib.Call("rqtx_init")
		}
		isInit = true
	})
}

func Rqtx(md5 string) uint32 {
	ensureInit()
	callMu.Lock()
	defer callMu.Unlock()

	//if lib != nil {
	//	if !isInit {
	//		_, _, err := lib.Call("rqtx_init")
	//		if err != nil {
	//			log.Error("DLL rqtx_init error: ", err)
	//		} else {
	//			isInit = true
	//		}
	//	}
	//	if isInit {
	//		md5Bytes := append([]byte(md5), 0)
	//		r1, _, err := lib.Call("rqtx", md5Bytes)
	//		if err == nil && r1 != 0 {
	//			return uint32(r1)
	//		}
	//		if err != nil {
	//			log.Error("DLL rqtx call error: ", err)
	//		}
	//	}
	//}
	return RQTX(md5)
}

func EncodeString(input string, constant uint32, timestamp uint32) string {
	callMu.Lock()
	defer callMu.Unlock()

	inputLen := uint32(len(input))
	buff := make([]byte, inputLen)
	copy(buff, []byte(input))
	out := make([]byte, inputLen)
	_, _, err := lib.Call("encode_cstr", buff, inputLen, out, constant, timestamp)
	if err == nil {
		return string(out)
	}
	_, _, err = lib.Call("encode_string", buff, inputLen, out, constant, timestamp)
	return string(out)
}

func EncodeUInt64(input uint64, constant uint32, timestamp uint32) uint64 {
	callMu.Lock()
	defer callMu.Unlock()

	input_v := input
	out := uint64(0)
	lib.Call("encode_int64", &input_v, timestamp, constant, &out)
	return out
}

func Si(config string, data string) string {
	callMu.Lock()
	defer callMu.Unlock()

	if lib != nil {
		configBytes := []byte(config)
		dataBytes := []byte(data)
		if len(configBytes) < 128 {
			configBytes = append(configBytes, make([]byte, 128-len(configBytes))...)
		} else if len(configBytes) > 128 {
			configBytes = configBytes[:128]
		}
		if len(dataBytes) < 128 {
			dataBytes = append(dataBytes, make([]byte, 128-len(dataBytes))...)
		}
		siOut := make([]byte, 33)
		_, _, err := lib.Call("si_cstr", configBytes, 128, dataBytes, len(dataBytes), siOut)
		if err != nil {
			_, _, err2 := lib.Call("si", configBytes, 128, dataBytes, len(dataBytes), siOut)
			if err2 != nil {
				return siCstrGo(config, data)
			}
		}
		outStr := ""
		for _, b := range siOut {
			if b == 0 {
				break
			}
			outStr += string(rune(b))
		}
		return outStr
	}
	return siCstrGo(config, data)
}

func vip08() {
	filePath := ""
	switch runtime.GOOS {
	case "windows":
		filePath = ".\\lib\\key"
	case "linux":
		filePath = "./lib/key"
	case "darwin":
		filePath = "./lib/key"
	default:
		panic("unsupported platform")
	}

	encodedData, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Println("失败了:", err)
		return
	}
	decodedMessage, err := base64.StdEncoding.DecodeString(string(encodedData))
	fmt.Println(string(decodedMessage))
}
