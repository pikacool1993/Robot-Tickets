package secure

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

func blob(data []byte) *windows.DataBlob {
	b := &windows.DataBlob{Size: uint32(len(data))}
	if len(data) > 0 {
		b.Data = &data[0]
	}
	return b
}

// DPAPI CurrentUser binds persisted secrets to this Windows user and computer.
func Protect(data []byte) ([]byte, error)   { return transform(data, false) }
func Unprotect(data []byte) ([]byte, error) { return transform(data, true) }
func transform(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty protected data")
	}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(blob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(blob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, errors.New("无法保护或读取本地敏感信息，请使用原 Windows 用户重新配置")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
