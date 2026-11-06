package v08

import "encoding/binary"

func siCstrGo(config string, data string) string {
	configBytes := []byte(config)
	dataBytes := []byte(data)

	stringArray := make([][]byte, 16)
	for i := 0; i < 16; i++ {
		start := i * 8
		end := start + 8
		chunk := make([]byte, 8)
		if start < len(configBytes) {
			if end > len(configBytes) {
				copy(chunk, configBytes[start:])
			} else {
				copy(chunk, configBytes[start:end])
			}
		}
		stringArray[i] = chunk
	}

	siSoftDataHashE(dataBytes, stringArray)

	output := make([]byte, 33)
	offset := 0
	for i := 0; i < 4 && offset < 32; i++ {
		for j := 0; j < 8 && offset < 32; j++ {
			if j < len(stringArray[i]) {
				output[offset] = stringArray[i][j]
			}
			offset++
		}
	}
	output[32] = 0

	outStr := ""
	for _, b := range output {
		if b == 0 {
			break
		}
		outStr += string(rune(b))
	}
	return outStr
}

func siSoftDataHashE(dataBytes []byte, stringArray [][]byte) {
	if len(dataBytes) < 6 {
		return
	}

	var dataPtr []byte
	if len(dataBytes) > 0x17 && dataBytes[0x17]&0x80 != 0 {
		ptrVal := binary.LittleEndian.Uint64(dataBytes[:8])
		if ptrVal < uint64(len(dataBytes)) {
			dataPtr = dataBytes[ptrVal:]
		} else {
			dataPtr = dataBytes
		}
	} else {
		dataPtr = dataBytes
	}

	byte4 := dataPtr[4]
	byte5 := dataPtr[5]

	startOffset := 4 + int(dataPtr[0]&0x03) + 0x0a

	ptrOffset := startOffset
	counter := 0

	for counter <= 8 {
		if ptrOffset >= len(dataPtr) {
			break
		}
		currentByte := dataPtr[ptrOffset]

		if currentByte == byte4 {
			siSoftDataSwap(stringArray, int(byte4), int(byte5))
		} else if currentByte == byte5 {
			siSoftDataXor(stringArray, int(byte4), int(byte5))
		} else {
			break
		}

		ptrOffset += 4
		counter++
	}
}

func siSoftDataSwap(stringArray [][]byte, idx1 int, idx2 int) {
	if idx1 > 0x0f || idx2 > 0x0f || idx1 == idx2 {
		return
	}

	tmp := make([]byte, len(stringArray[idx1]))
	copy(tmp, stringArray[idx1])
	copy(stringArray[idx1], stringArray[idx2])
	copy(stringArray[idx2], tmp)
}

func siSoftDataXor(stringArray [][]byte, idx1 int, idx2 int) {
	if idx1 > 0x0f || idx2 > 0x0f {
		return
	}

	len1 := len(stringArray[idx1])
	len2 := len(stringArray[idx2])
	minLen := len1
	if len2 < minLen {
		minLen = len2
	}

	for i := 0; i < minLen; i++ {
		stringArray[idx2][i] ^= stringArray[idx1][i]
	}
}
