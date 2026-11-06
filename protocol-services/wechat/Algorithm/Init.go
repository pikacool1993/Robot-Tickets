package Algorithm

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
)

func (h *Client) Init(Model string) {

	h.curve = elliptic.P256()
	h.clientHash = sha256.New()
	h.serverHash = sha256.New()

	if Model == "IOS" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = IPadVersion
		h.DeviceType = IPadDeviceType
		h.InitPubKey, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	if Model == "iPhone" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = IPhoneVersion
		h.DeviceType = IPhoneDeviceType
		h.InitPubKey, _ = hex.DecodeString("04f684807d4b3c772dad0d85ac1f906d93a5b14acd5034090e3d0735c650baeb1ab5e81378bf2397f7c2a8ddf04cd6c680965a5cba95ccda7021ee969d43ce8fd6")
		h.InitPubKey10000, _ = hex.DecodeString("041015df8ae2bcc29b17bcc4e62bdda9167b232f479f1d3e257194b931938454b20f2cf278883aed7743bde051bdecd0d0b5e2709b15840c37aff14c3578ea2df0")
	}

	if Model == "MAC" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = MacVersion
		h.DeviceType = MacDeviceType
		h.InitPubKey, _ = hex.DecodeString("043927825e0fdc4fcdf57e92459024a4f3eab53d4cac95c5d9c6411dd2af2e93e7cf673919210c6f8d91a856898855273275d553e9624d8b15302a68338954d444")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	if Model == "AndroidPad" {
		h.Status = HYBRID_ENC
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = AndroidPadVersion
		h.DeviceType = AndroidPadDeviceType
		h.InitPubKey, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	if Model == "Windows" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = WinVersion
		h.DeviceType = WinDeviceType
		h.InitPubKey, _ = hex.DecodeString("043927825e0fdc4fcdf57e92459024a4f3eab53d4cac95c5d9c6411dd2af2e93e7cf673919210c6f8d91a856898855273275d553e9624d8b15302a68338954d444")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	if Model == "WindowsUwp" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = WinUwpVersion
		h.DeviceType = WinDeviceType
		h.InitPubKey, _ = hex.DecodeString("043927825e0fdc4fcdf57e92459024a4f3eab53d4cac95c5d9c6411dd2af2e93e7cf673919210c6f8d91a856898855273275d553e9624d8b15302a68338954d444")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	if Model == "WinUnified" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.DeviceType = WinUnifiedDeviceType
		h.InitPubKey, _ = hex.DecodeString("043927825e0fdc4fcdf57e92459024a4f3eab53d4cac95c5d9c6411dd2af2e93e7cf673919210c6f8d91a856898855273275d553e9624d8b15302a68338954d444")
		h.InitPubKey10000, _ = hex.DecodeString("04bc3abd45083d868df672d246675f8ab44164b0148510408e0a7ba1a913cf5d0b66bd4ad4b55bdf21d1689a692b83f23277cbd831d0ddb4ef0691ea02909e8504")
	}

	if Model == "Android" {
		h.Status = HYBRID_ENC
		h.Version = AndroidVersion
		h.DeviceType = AndroidDeviceType
		h.InitPubKey, _ = hex.DecodeString("0495BC6E5C1331AD172D0F35B1792C3CE63F91572ABD2DD6DF6DAC2D70195C3F6627CCA60307305D8495A8C38B4416C75021E823B6C97DFFE79C14CB7C3AF8A586")
	}

	if Model == "Car" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = CarVersion
		h.DeviceType = CarDeviceType
		h.InitPubKey, _ = hex.DecodeString("043927825e0fdc4fcdf57e92459024a4f3eab53d4cac95c5d9c6411dd2af2e93e7cf673919210c6f8d91a856898855273275d553e9624d8b15302a68338954d444")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

}
func (h *Client) Init2(Model string) {

	h.curve = elliptic.P256()
	h.clientHash = sha256.New()
	h.serverHash = sha256.New()

	//MicroMessenger/
	//7.0.12(0x17000c21)--wxid_4ucwxdg2896t222
	//MicroMessenger/7.0.14(0x17000e2e) --- 自由改版本
	//MicroMessenger/7.0.15(0x17000f26)
	//MicroMessenger/7.0.17(0x17001124)   iphone  0X1700112a
	if Model == "IOS" {
		h.Privkey, h.PubKey = GetECDH415Key()
		h.Version = IPadVersion
		h.DeviceType = IPadDeviceType
		h.InitPubKey, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
		h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	}

	////安卓版本号
	if Model == "Android" {
		h.Status = HYBRID_ENC
		h.Version = AndroidVersion
		h.DeviceType = AndroidDeviceType
		h.InitPubKey, _ = hex.DecodeString("0468116828474de3c798d027bae6b71bcc0ce83b8383ee2c0ea9b57eb968e89b412e0e592d5e96c7c1708373bf4fe91280e52b8643196cdd9a4ac2206e5c79cc86")
		h.InitPubKey10000, _ = hex.DecodeString("04bcd3b9b231330284295f55951e624433bb0d9f87791918b1ecf2d7b5a53ec2b4392e0084c31bbab3b85c1141cc31373d4dcd88fb572e24c68b8124f7437c8a5e")
	}

}

func (h *WinUClient) Init() {
	h.curve = elliptic.P256()
	h.clientHash = sha256.New()
	h.serverHash = sha256.New()

	h.Privkey, h.PubKey = GetECDH415Key()
	h.Version = WinUnifiedVersion
	h.DeviceType = WinUnifiedDeviceType
	h.InitPubKey, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")
	h.InitPubKey10000, _ = hex.DecodeString("047ebe7604acf072b0ab0177ea551a7b72588f9b5d3801dfd7bb1bca8e33d1c3b8fa6e4e4026eb38d5bb365088a3d3167c83bdd0bbb46255f88a16ede6f7ab43b5")

}
