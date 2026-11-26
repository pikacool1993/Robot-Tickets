package orderalarm

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

var mciSendString = windows.NewLazySystemDLL("winmm.dll").NewProc("mciSendStringW")

type NativePlayer struct {
	audio  []byte
	path   string
	opened bool
}

func NewNativePlayer(audio []byte) *NativePlayer { return &NativePlayer{audio: audio} }
func command(s string) error {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return err
	}
	result, _, _ := mciSendString.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0)
	if result != 0 {
		return errors.New("audio device command failed")
	}
	return nil
}
func (p *NativePlayer) Loop() error {
	if p.path == "" {
		f, err := os.CreateTemp("", "robot-order-*.mp3")
		if err != nil {
			return err
		}
		p.path = f.Name()
		_, err = f.Write(p.audio)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if !p.opened {
		if err := command(`open "` + p.path + `" type mpegvideo alias robot_order_alarm`); err != nil {
			return err
		}
		p.opened = true
	}
	return command("play robot_order_alarm from 0 repeat")
}
func (p *NativePlayer) Stop() error {
	if !p.opened {
		return nil
	}
	return command("stop robot_order_alarm")
}
func (p *NativePlayer) Close() error {
	if p.opened {
		_ = command("close robot_order_alarm")
		p.opened = false
	}
	if p.path != "" {
		return os.Remove(p.path)
	}
	return nil
}
