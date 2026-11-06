package secure_random

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	mrand "math/rand"
	"sync"
	"time"
)

var (
	globalRand *mrand.Rand
	once       sync.Once
)

func initRand() {
	var seed int64
	buf := make([]byte, 8)
	if _, err := crand.Read(buf); err == nil {
		seed = int64(binary.BigEndian.Uint64(buf))
	} else {
		seed = time.Now().UnixNano()
	}
	globalRand = mrand.New(mrand.NewSource(seed))
}

func getRand() *mrand.Rand {
	once.Do(initRand)
	return globalRand
}

func SecureIntn(n int) int {
	if n <= 0 {
		return 0
	}
	bigN := big.NewInt(int64(n))
	result, err := crand.Int(crand.Reader, bigN)
	if err != nil {
		return getRand().Intn(n)
	}
	return int(result.Int64())
}

func SecureInt63() int64 {
	buf := make([]byte, 8)
	if _, err := crand.Read(buf); err != nil {
		return getRand().Int63()
	}
	return int64(binary.BigEndian.Uint64(buf) & 0x7FFFFFFFFFFFFFFF)
}

func SecureRandBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := crand.Read(buf); err != nil {
		getRand().Read(buf)
	}
	return buf
}

func SecureRandHex(n int) string {
	b := SecureRandBytes(n)
	return fmt.Sprintf("%x", b)
}

func JitterMs(baseMs int, jitterPercent int) time.Duration {
	if jitterPercent <= 0 {
		jitterPercent = 10
	}
	jitter := baseMs * jitterPercent / 100
	if jitter <= 0 {
		jitter = 1
	}
	offset := SecureIntn(jitter*2) - jitter
	return time.Duration(baseMs+offset) * time.Millisecond
}

func HumanDelay(minMs, maxMs int) time.Duration {
	if maxMs <= minMs {
		return time.Duration(minMs) * time.Millisecond
	}
	range_ := maxMs - minMs
	val := SecureIntn(range_)
	return time.Duration(minMs+val) * time.Millisecond
}

func ExponentialBackoff(attempt int, baseMs int, maxMs int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := baseMs << uint(attempt)
	if delay > maxMs {
		delay = maxMs
	}
	return JitterMs(delay, 25)
}

func ShuffleSlice(slice []string) {
	n := len(slice)
	for i := n - 1; i > 0; i-- {
		j := SecureIntn(i + 1)
		slice[i], slice[j] = slice[j], slice[i]
	}
}

func PickRandom(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	return slice[SecureIntn(len(slice))]
}

func SecureUint32() uint32 {
	buf := make([]byte, 4)
	if _, err := crand.Read(buf); err != nil {
		return getRand().Uint32()
	}
	return binary.BigEndian.Uint32(buf)
}

func SecureInt32() int32 {
	return int32(SecureUint32())
}

func SecureUint64() uint64 {
	buf := make([]byte, 8)
	if _, err := crand.Read(buf); err != nil {
		return getRand().Uint64()
	}
	return binary.BigEndian.Uint64(buf)
}

func SecureInt64() int64 {
	return int64(SecureUint64())
}

func WeightedChoice(weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return 0
	}
	r := SecureIntn(total)
	sum := 0
	for i, w := range weights {
		sum += w
		if r < sum {
			return i
		}
	}
	return len(weights) - 1
}
