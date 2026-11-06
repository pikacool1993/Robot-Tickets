package Algorithm

import (
	"github.com/golang/protobuf/proto"
	srand "wechatdll/clientsdk/securerandom"
	"wechatdll/Cilent/mm"
)

func wcstf_and_wcste_with_count(count, time_interval int) (mm.Wcstf, mm.Wcste) {
	rs := srand.SecureIntn(60) + 60
	tes := time_interval - rs
	ten := time_interval
	tfs := (time_interval - rs + srand.SecureIntn(1) + 3) * 1000
	wcstf := mm.Wcstf{}
	wcstf.StartTime = proto.Uint64(uint64(tfs))
	for i := 0; i <= count; i++ {
		tfs = tfs + srand.SecureIntn(500) + 500
		wcstf.EndTime = append(wcstf.EndTime, uint64(tfs))
	}
	tfn := tfs + (srand.SecureIntn(7)+3)*1000 + srand.SecureIntn(1000)
	wcstf.CheckTime = proto.Uint64(uint64(tfn))
	wcstf.Count = proto.Uint32(uint32(count))
	wcste := mm.Wcste{}
	wcste.Checkid = proto.String("4")
	wcste.StartTime = proto.Uint32(uint32(tes))
	wcste.CheckTime = proto.Uint32(uint32(ten))
	wcste.Count1 = proto.Uint32(uint32(srand.SecureIntn(100)))
	wcste.Count2 = proto.Uint32(uint32(srand.SecureIntn(3000) + 2000))
	return wcstf, wcste
}
