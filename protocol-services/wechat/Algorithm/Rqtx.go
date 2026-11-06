package Algorithm

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
)

var (
	t2 = [16]uint32{
		0x324F3C11, 0xBB1AA33D, 0xE1DDB4CA, 0xA49CEF53,
		0xBEBCD4D4, 0xF2F7ABD2, 0x6D0EE65D, 0x4AC835DD,
		0x5062AC08, 0x69A086DB, 0x79E5E7A5, 0x75D33087,
		0xB49F7EFB, 0xE2C098A9, 0x006FDAE4, 0x6057E875,
	}

	t1 = [16]uint32{
		0x25CA5B3D, 0xED3A3A33, 0x63057C8C, 0x6B687869,
		0x4CA1666D, 0x93BF5B03, 0xE1C2B6A9, 0xCE8B2B78,
		0x7BEF2B4E, 0x1893D6CE, 0xF3279663, 0xEAD788A3,
		0x61B85E9E, 0xBB4FAA76, 0xB3FC9498, 0x99606205,
	}

	f1 = [64]uint32{
		0x7DAD15E3, 0xB667029E, 0xC9911176, 0x35A51C6F, 0x8EF515A1, 0xE330660D, 0x46DA9199, 0x68C8DFA8, 0xB46CEAE0, 0x93C3BE6E,
		0xC9E81BCD, 0x57F52B15, 0x77D4C32A, 0x7B0925F2, 0x9891AD29, 0xB6B31FD8, 0x566E3032, 0x7DDE60EF, 0xC1E2AD2A, 0x259C82E0,
		0x28EED8FB, 0x060AB4C0, 0xA7F9BE6A, 0x8AD14304, 0x7BF2208A, 0xB9781B59, 0x45269C50, 0xDE1C3CAB, 0xB9FE3450, 0x4D6FF1DC,
		0x151A5E92, 0x5E69136C, 0x8C56C171, 0x9E3A7DA3, 0x67DAEFE5, 0x3D724638, 0x0DA781B7, 0x33813E62, 0x1062B7C8, 0xEAF61FAC,
		0x0073983C, 0xF87198A7, 0x3F126BEE, 0xD25725DA, 0x4A78B169, 0xFB75C479, 0x781EE971, 0xE426229F, 0x5FF33BE6, 0xA4B0DA31,
		0x6F2D4114, 0x76315127, 0xFAC1DE71, 0x29D9A8F2, 0x1EB1F7A4, 0x997812EE, 0x743BC13C, 0x62FCA14F, 0xA180CD80, 0x38A339F7,
		0x1A356AEB, 0x68E2AEBB, 0x23AF07AA, 0x295872B5,
	}

	f2 = [64]uint32{
		0x867DA32F, 0x35E81448, 0x67FA0F23, 0x1219E6DD, 0x0300E789, 0xBFDDF23A, 0x08E6546E, 0x93606ABC, 0x228B830F, 0x6527DF65,
		0xFEEDC7D5, 0xA2AB7BC9, 0x603275C2, 0x793D07BE, 0x3745F647, 0xF359BCA8, 0x7B4819BF, 0xF70E9109, 0x4E2CEE01, 0x7EE03E8F,
		0xA3447276, 0x93D9C234, 0x8225A9CC, 0xB5496671, 0x70585E70, 0x73CAE0FD, 0x055963BB, 0x2C817136, 0xD887103A, 0x0124D3A9,
		0xF13C6004, 0x08EA6849, 0xAE77F5F9, 0xE09D1934, 0x13DFCA2B, 0x19E40F52, 0x92D382C4, 0x5AFB183C, 0x3DF06627, 0x7B2E3FB2,
		0x6FDD1273, 0xCDB6A087, 0x965FCE6B, 0x155BB878, 0xEE8C9CF7, 0xE35E0BBF, 0x27A33254, 0x7DE86C22, 0x09A4A19D, 0x63D12E76,
		0x0323C1CB, 0xF2C36FD1, 0x0D2EE1CB, 0xD17EA020, 0xD7F2A082, 0x2A4D4BF2, 0x417120BD, 0x72E4457C, 0xAA7CAFEB, 0xCD85E8D7,
		0xBA563274, 0x59FBDC32, 0x7F562EC0, 0xE9B8137C,
	}
)

func ROR(s uint32, c uint8) uint32 {
	return (s >> c) | (s << (32 - c))
}

func genT1(s [16]uint32) [64]uint32 {

	var r [64]uint32
	for i := 0; i < 16; i++ {
		r[i] = ROR(s[i], 8)
	}

	for i := 16; i < 64; i++ {
		R3 := r[14+i-16]
		R5 := ROR(R3, 5)
		R6 := ROR(R3, 9)

		R7 := r[1+i-16]
		R8 := r[0+i-16]
		R9 := r[9+i-16]

		R3 >>= 2
		R3 ^= R5

		R5 = ROR(R7, 3)
		RA := ROR(R7, 11)
		R7 >>= 5
		R7 ^= RA
		R5 ^= R7
		R3 ^= R6

		R4 := R9
		R6 = R8
		R4 += R6
		R3 += R4
		R3 += R5

		r[i] = R3
	}

	return r
}

/*func genT1(s [16]uint32) [64]uint32 {

	var r [64]uint32
	for i := 0; i < 16; i++ {
		r[i] = ROR(s[i], 8)
	}

	for i := 16; i < 64; i++ {
		R3 := r[14+i-16]
		R5 := ROR(R3, 5)
		R6 := ROR(R3, 9)

		R7 := r[1+i-16]
		R8 := r[0+i-16]
		R9 := r[9+i-16]

		R3 >>= 2
		R3 ^= R5

		R5 = ROR(R7, 3)
		RA := ROR(R7, 11)
		R7 >>= 5
		R7 ^= RA
		R5 ^= R7
		R3 ^= R6

		R4 := R9
		R6 = R8
		R4 += R6
		R3 += R4
		R3 += R5

		r[i] = R3
	}

	return r
}
*/
// RQTXHASH struct
type RQTXHASH struct {
	A1, A2, A3, A4, A5, A6, A7, A8, A9, A10 uint32

	mt [64]uint32
	ft [64]uint32

	state [8]uint32
}

func (r *RQTXHASH) init() {

	r.state[0] = 0x9592E557
	r.state[1] = 0xFB1566EE
	r.state[2] = 0x763D2E43
	r.state[3] = 0x205E4046
	r.state[4] = 0xB0984A2D
	r.state[5] = 0x6A645664
	r.state[6] = 0xCEDA8268
	r.state[7] = 0xB7084E8C
}

/*func (r *RQTXHASH) update(input [16]uint32) {

	r.A1 = r.state[0]
	r.A2 = r.state[1]
	r.A3 = r.state[2]
	r.A4 = r.state[3]
	r.A5 = r.state[4]
	r.A6 = r.state[5]
	r.A7 = r.state[6]
	r.A8 = r.state[7]

	r.mt = genT1(input)

	for i := 0; i < 64; i++ {

		if i%3 == 0 {
			r.ft = f1
		} else {
			r.ft = f2
		}

		R4 := ROR(r.A5, 7)
		R6 := ROR(r.A5, 13)

		R2 := R4 ^ R6

		R8 := r.A5 & r.A6
		R20 := r.A5 ^ 0xffffffff
		R20 &= r.A7

		R4 = ROR(r.A5, 17)
		R2 ^= R4
		R4 = R20 | R8
		R4 += r.A8

		R4 += r.mt[i]
		R2 += R4

		R1 := R2 + r.ft[i]

		R2 = ROR(r.A1, 5)
		R4 = ROR(r.A1, 11)
		R2 ^= R4

		R4 = ROR(r.A1, 19)
		R6 = r.A4 + R1

		r.A9 = R6

		R2 ^= R4
		R4 = r.A3 ^ r.A2
		R4 &= r.A1

		R6 = r.A3 & r.A2
		R4 ^= R6
		R2 += R4
		R1 += R2

		r.A10 = R1

		T1 := r.A1
		r.A1 = r.A10

		T2 := r.A2
		r.A2 = T1

		r.A8 = r.A7
		r.A7 = r.A6
		r.A6 = r.A5
		r.A5 = r.A9
		r.A4 = r.A3
		r.A3 = T2
	}

	r.state[0] += r.A8
	r.state[1] += r.A7
	r.state[2] += r.A6
	r.state[3] += r.A5
	r.state[4] += r.A4
	r.state[5] += r.A3
	r.state[6] += r.A2
	r.state[7] += r.A1
}*/

func (r *RQTXHASH) update(input [16]uint32) {

	r.A1 = r.state[0]
	r.A2 = r.state[1]
	r.A3 = r.state[2]
	r.A4 = r.state[3]
	r.A5 = r.state[4]
	r.A6 = r.state[5]
	r.A7 = r.state[6]
	r.A8 = r.state[7]

	r.mt = genT1(input)

	for i := 0; i < 64; i++ {

		if i%3 == 0 {
			r.ft = f1
		} else {
			r.ft = f2
		}

		R4 := ROR(r.A5, 7)
		R6 := ROR(r.A5, 13)

		R2 := R4 ^ R6

		R8 := r.A5 & r.A6
		R20 := r.A5 ^ 0xffffffff
		R20 &= r.A7

		R4 = ROR(r.A5, 17)
		R2 ^= R4
		R4 = R20 | R8
		R4 += r.A8

		R4 += r.mt[i]
		R2 += R4

		R1 := R2 + r.ft[i]

		R2 = ROR(r.A1, 5)
		R4 = ROR(r.A1, 11)
		R2 ^= R4

		R4 = ROR(r.A1, 19)
		R6 = r.A4 + R1

		r.A9 = R6

		R2 ^= R4
		R4 = r.A3 ^ r.A2
		R4 &= r.A1

		R6 = r.A3 & r.A2
		R4 ^= R6
		R2 += R4
		R1 += R2

		r.A10 = R1

		T1 := r.A1
		r.A1 = r.A10

		T2 := r.A2
		r.A2 = T1

		r.A8 = r.A7
		r.A7 = r.A6
		r.A6 = r.A5
		r.A5 = r.A9
		r.A4 = r.A3
		r.A3 = T2
	}

	r.state[0] += r.A8
	r.state[1] += r.A7
	r.state[2] += r.A6
	r.state[3] += r.A5
	r.state[4] += r.A4
	r.state[5] += r.A3
	r.state[6] += r.A2
	r.state[7] += r.A1
}

func (r *RQTXHASH) getState() []byte {

	rc := new(bytes.Buffer)
	binary.Write(rc, binary.LittleEndian, r.A1)
	binary.Write(rc, binary.LittleEndian, r.A2)
	binary.Write(rc, binary.LittleEndian, r.A3)
	binary.Write(rc, binary.LittleEndian, r.A4)
	binary.Write(rc, binary.LittleEndian, r.A5)
	binary.Write(rc, binary.LittleEndian, r.A6)
	binary.Write(rc, binary.LittleEndian, r.A7)
	binary.Write(rc, binary.LittleEndian, r.A8)
	return rc.Bytes()
}

func (r *RQTXHASH) final() []byte {
	rc := new(bytes.Buffer)
	binary.Write(rc, binary.BigEndian, r.state[0])
	binary.Write(rc, binary.BigEndian, r.state[1])
	binary.Write(rc, binary.BigEndian, r.state[2])
	binary.Write(rc, binary.BigEndian, r.state[3])
	binary.Write(rc, binary.BigEndian, r.state[4])
	binary.Write(rc, binary.BigEndian, r.state[5])
	binary.Write(rc, binary.BigEndian, r.state[6])
	binary.Write(rc, binary.BigEndian, r.state[7])
	return rc.Bytes()
}

//
/*
func trans(input []byte) []byte {
	var BB [16]uint32
	salt := [4]uint32{0x0E16D797, 0xF225913B, 0x59FE86CB, 0x413B5EBE}
	for i := 0; i < 16; i++ {
		BB[i] = uint32(input[4*i+2]) | (uint32(input[4*i+1]) << 8) | (uint32(input[4*i]) << 16) | (uint32(input[4*i+3]) << 24)
	}

	R8 := salt[0] + BB[0] + (salt[1] ^ salt[2] ^ salt[3]) + 0xa4bdc6e9
	R8 = ROR(R8, 7)
	R8 += salt[1]

	R9 := salt[1] ^ salt[2]
	R9 ^= R8

	RA := salt[3] + BB[1]
	R9 += RA
	R9 += 0x554e80f9
	R9 = ROR(R9, 12)
	R9 += R8

	RB := salt[2] + BB[2]
	RC := R9 ^ R8
	RD := RC ^ salt[1]
	RB += RD
	RB += 0xd3d89f33
	RB = ROR(RB, 17)
	RB += R9

	RC ^= RB
	RD = salt[1] + BB[3]
	RC += RD
	RC += 0x15a2af88
	RC = ROR(RC, 22)

	R8 += BB[4]
	RC += RB
	RE := RC ^ RB
	RF := RE ^ R9
	R8 += RF
	R8 += 0x924dac27
	R8 = ROR(R8, 7)

	R9 += BB[5]
	R8 += RC
	RE ^= R8
	R9 += RE
	R9 += 0x7e563339
	R9 = ROR(R9, 12)

	RB += BB[6]
	R9 += R8
	RE = R9 ^ R8
	RF = RE ^ RC
	RB += RF
	RB += 0xf6bae427
	RB = ROR(RB, 17)

	RC += BB[7]
	RB += R9
	RE ^= RB
	RC += RE
	RC += 0x12132474
	RC = ROR(RC, 22)

	R8 += BB[8]
	RC += RB
	RE = RC ^ RB
	RF = RE ^ R9
	R8 += RF
	R8 += 0x9436b72e
	R8 = ROR(R8, 7)

	R9 += BB[9]
	R8 += RC
	RE ^= R8
	R9 += RE
	R9 += 0x8B366D12
	R9 = ROR(R9, 12)

	RB += BB[10]
	R9 += R8
	RE = R8 ^ RC
	RE ^= R9
	RB += RE
	RB += 0x23e22dcc
	RB = ROR(RB, 17)

	RC += BB[11]
	RB += R9
	RE = R9 ^ R8
	RE ^= RB
	RC += RE
	RC += 0x69ab94c8
	RC = ROR(RC, 22)

	R8 += BB[12]
	RC += RB
	RE = RB ^ R9
	RE ^= RC
	R8 += RE
	R8 += 0xcfdb953b
	R8 = ROR(R8, 7)

	R9 += BB[13]
	R8 += RC
	RE = RC ^ RB
	RE ^= R8
	R9 += RE
	R9 += 0xe3c702cf
	R9 = ROR(R9, 12)

	RA = RB + BB[14]
	R9 += R8
	RB = R8 ^ RC
	RB ^= R9
	RA += RB
	RA += 0x48233422
	RA = ROR(RA, 17)

	RB = RC + BB[15]
	RA += R9
	RC = R9 ^ R8
	RC ^= RA
	RB += RC
	RB += 0xaad28857
	RB = ROR(RB, 22)

	//round 2
	RB += RA
	RC = RB ^ 0xFFFFFFFF
	RC &= RA
	RD = RB & R9
	RC |= RD
	R8 += BB[1]
	R8 += RC
	R8 += 0x8de587b3
	R8 = ROR(R8, 5)

	R8 += RB
	RC = R8 ^ 0xFFFFFFFF
	RC &= RB
	RD = R8 & RA
	RC |= RD
	R9 += BB[6]
	R9 += RC
	R9 += 0x8f209322
	R9 = ROR(R9, 9)

	R9 += R8
	RD = R9 ^ 0xFFFFFFFF
	RD &= R8
	RE = R9 & RB
	RD |= RE
	RA += BB[11]
	RA += RD
	RA += 0xaed47066
	RA = ROR(RA, 14)
	RA += R9

	RE = ^RA
	RE &= R9
	RF = RA & R8
	RE |= RF
	RB += BB[0]
	RB += RE
	RB += 0x5e211c2c
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 := RB & R9
	RF |= R10
	R8 += BB[5]
	R8 += RF
	R8 += 0xac5e94b2
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[10]
	R9 += RF
	R9 += 0xfa747ca2
	R9 = ROR(R9, 9)
	R9 += R8

	RF = ^R9
	RF &= R8
	R10 = R9 & RB
	RF |= R10
	RA += BB[15]
	RA += RF
	RA += 0x674b87b8
	RA = ROR(RA, 14)
	RA += R9

	RF = ^RA
	RF &= R9
	R10 = RA & R8
	RF |= R10
	RB += BB[4]
	RB += RF
	RB += 0xc4d2faa2
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 = RB & R9
	RF |= R10
	R8 += BB[9]
	R8 += RF
	R8 += 0x2f1b9b0e
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[14]
	R9 += RF
	R9 += 0x1a55a83a
	R9 = ROR(R9, 9)
	R9 += R8

	RF = ^R9
	RF &= R8
	R10 = R9 & RB
	RF |= R10
	RA += BB[3]
	RA += RF
	RA += 0x91bf8078
	RA = ROR(RA, 14)
	RA += R9

	RF = ^RA
	RF &= R9
	R10 = RA & R8
	RF |= R10
	RB += BB[8]
	RB += RF
	RB += 0x86b23cfc
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 = RB & R9
	RF |= R10
	R8 += BB[13]
	R8 += RF
	R8 += 0x7c24dd24
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[2]
	R9 += RF
	R9 += 0x25ac16a7
	R9 = ROR(R9, 9)
	R9 += R8

	RC = R9 & RB
	RF = ^R9
	R10 = R8 & RF
	RC |= R10
	RA += BB[7]
	RA += RC
	RA += 0xd88f1be9
	RA = ROR(RA, 14)
	RA += R9

	RC = RA & R8
	RD = ^RA
	R10 = R9 & RD
	RC |= R10
	RB += BB[12]
	RB += RC
	RB += 0x8a19fa0b
	RB = ROR(RB, 20)
	RB += RA

	//ROUND3
	RC = RB & RF
	RE = RA & R9
	RC |= RE
	R8 += BB[5]
	R8 += RC
	R8 += 0x813c91ac
	R8 = ROR(R8, 4)
	R8 += RB

	RC = R8 & RD
	RD = RB & RA
	RC |= RD
	R9 += BB[8]
	R9 += RC
	R9 += 0xcf6eb59d
	R9 = ROR(R9, 11)
	R9 += R8

	RC = ^RB
	RC &= R9
	RD = R8 & RB
	RC |= RD
	RA += BB[11]
	RA += RC
	RA += 0xafdbd5e2
	RA = ROR(RA, 16)
	RA += R9

	RD = ^R8
	RD &= RA
	RE = R9 & R8
	RD |= RE
	RB += BB[14]
	RB += RD
	RB += 0x80e7c964
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[1]
	R8 += RE
	R8 += 0xe15e5e35
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[4]
	R9 += RE
	R9 += 0xfbd6b4af
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[7]
	RA += RE
	RA += 0xbc162b09
	RA = ROR(RA, 16)
	RA += R9

	RE = ^R8
	RE &= RA
	RF = R9 & R8
	RE |= RF
	RB += BB[10]
	RB += RE
	RB += 0x35f562aa
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[13]
	R8 += RE
	R8 += 0x27ef30d3
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[0]
	R9 += RE
	R9 += 0x5ea6b3b5
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[3]
	RA += RE
	RA += 0x299cca2a
	RA = ROR(RA, 16)
	RA += R9

	RE = ^R8
	RE &= RA
	RF = R9 & R8
	RE |= RF
	RB += BB[6]
	RB += RE
	RB += 0xc56e9540
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[9]
	R8 += RE
	R8 += 0x1f269216
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[12]
	R9 += RE
	R9 += 0x98ca2340
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[15]
	RA += RE
	RA += 0xd02ead4d
	RA = ROR(RA, 16)
	RA += R9

	RC = R8 + BB[0]

	RE = ^R8
	R8 &= R9
	RE &= RA
	R8 |= RE
	RB += BB[2]
	R8 += RB
	R8 += 0x5bbad257
	R8 = ROR(R8, 23)
	R8 += RA

	RB = R9 + BB[7]

	//ROUND4
	RD = ^R8
	R9 |= RD
	R9 ^= RA
	R9 += RC
	R9 += 0xe6b95e33
	R9 = ROR(R9, 6)
	R9 += R8

	R5 := RA + BB[14]

	RD = ^R9
	RA |= RD
	RA ^= R8
	RA += RB
	RA += 0xedecac7a
	RA = ROR(RA, 10)
	RA += R9

	RD = R8 + BB[5]

	RE = ^RA
	R8 |= RE
	R8 ^= R9
	R5 += R8
	R5 += 0xedb098d7
	R5 = ROR(R5, 15)
	R5 += RA

	RE = R9 + BB[12] //?D238

	RF = ^R5
	R9 |= RF
	R9 ^= RA
	R9 += RD
	R9 += 0x21149206
	R9 = ROR(R9, 21)
	R9 += R5

	RF = RA + BB[3]

	R10 = ^R9
	RA |= R10
	RA ^= R5
	RA += RE
	RA += 0x4dd44b9a
	RA = ROR(RA, 6)
	RA += R9

	RE = R5 + BB[10] //D2C0

	R10 = ^RA
	R5 |= R10
	R5 ^= R9
	R5 += RF
	R5 += 0x1d14b76d
	R5 = ROR(R5, 10)
	R5 += RA

	R4 := R9 + BB[1]

	RF = ^R5
	R9 |= RF
	R9 ^= RA
	R9 += RE
	R9 += 0xa971bdae
	R9 = ROR(R9, 15)
	R9 += R5

	RE = RA + BB[8]

	RF = ^R9
	RA |= RF
	RA ^= R5
	R4 += RA
	R4 += 0x41499ee4
	R4 = ROR(R4, 21)
	R4 += R9

	R2 := R5 + BB[15] //D374

	RA = ^R4
	R5 |= RA
	R5 ^= R9
	R5 += RE
	R5 += 0xbecda772
	R5 = ROR(R5, 6)
	R5 += R4

	R7 := R9 + BB[6]

	RA = ^R5
	R9 |= RA
	R9 ^= R4
	R2 += R9
	R2 += 0x1a5dcda7
	R2 = ROR(R2, 10)
	R2 += R5

	R9 = R4 + BB[13] //D3EC

	RA = ^R2
	R4 |= RA
	R4 ^= R5
	R4 += R7
	R4 += 0x89adf877
	R4 = ROR(R4, 15)
	R4 += R2

	R3 := R5 + BB[4]

	R7 = ^R4
	R5 |= R7
	R5 ^= R2
	R5 += R9
	R5 += 0xbeab3f78
	R5 = ROR(R5, 21)
	R5 += R4

	R7 = R2 + BB[11] //D464

	R9 = ^R5
	R2 |= R9
	R2 ^= R4
	R2 += R3
	R2 += 0xc9579d4f
	R2 = ROR(R2, 6)
	R2 += R5

	R3 = ^R2
	R3 |= R4
	R3 ^= R5
	R3 += R7
	R3 += 0xc708d7b8
	R3 = ROR(R3, 10)
	R3 += R2

	R7 = R3 + salt[0]

	R1 := R4 + BB[2]
	R4 = R5 + BB[9] //D4F4

	R6 := ^R3
	R5 |= R6
	R5 ^= R2
	R1 += R5
	R1 += 0xa2dba837
	R1 = ROR(R1, 15)
	R1 += R3

	R5 = R2 + salt[3]

	R6 = ^R1
	R2 |= R6

	R6 = R1 + salt[2]
	R1 = R1 + salt[1]

	R2 ^= R3
	R2 += R4
	R2 += 0x5cbd8717
	R2 = ROR(R2, 21)
	R1 += R2

	dd := new(bytes.Buffer)
	binary.Write(dd, binary.LittleEndian, R7)
	binary.Write(dd, binary.LittleEndian, R1)
	binary.Write(dd, binary.LittleEndian, R6)
	binary.Write(dd, binary.LittleEndian, R5)

	return dd.Bytes()
}

func CalcCRC(input []byte) uint32 {

	R1 := uint32(input[1])
	R2 := uint32(input[0])

	R2 *= 0x85
	R2 += R1

	R3 := uint32(input[2])
	R3 &= 0xff
	R1 *= 0x85
	R1 += R3
	R1 *= 0x85
	R2 &= 0xff
	R2 *= 0x85
	R2 += R3
	R2 *= 0x85

	R4 := uint32(input[3])
	R4 &= 0xff
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85

	R4 = uint32(input[4])
	R4 &= 0xFF
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85

	R4 = uint32(input[5])
	R4 &= 0xFF
	R3 += R4
	R2 += R4
	R1 *= 0x85
	R1 += R4

	R4 = uint32(input[6])
	R4 &= 0xFF
	R1 *= 0x85
	R2 *= 0x85
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4

	R4 = uint32(input[7])
	R4 &= 0xFF
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4

	R4 = uint32(input[8])
	R4 &= 0xFF
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85
	R3 += R4
	R2 += R4
	R1 *= 0x85
	R1 += R4

	R4 = uint32(input[9])
	R4 &= 0xFF
	R1 *= 0x85
	R2 *= 0x85
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4

	R4 = uint32(input[10])
	R4 &= 0xFF
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4

	R4 = uint32(input[11])
	R4 &= 0xFF
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 += R4

	R6 := uint32(input[12])
	R6 &= 0xFF

	R5 := uint32(input[13])
	R5 &= 0xFF

	R4 = uint32(input[14])
	R4 &= 0xFF

	R1 *= 0x85
	R1 += R6
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R5
	R1 *= 0x85
	R1 += R4

	R1 <<= 8
	R1 &= 0x6F00

	R2 *= 0x85
	R2 += R6
	R3 *= 0x85
	R3 += R6
	R3 *= 0x85
	R3 += R5
	R2 *= 0x85
	R2 += R5
	R2 &= 0x3F
	R1 |= R2

	R2 = R3 & 0xFF
	R2 *= 0x85
	R2 += R4

	R0 := uint32(input[15])
	R2 *= 0x85
	R0 += R2
	R0 <<= 16
	R0 &= 0x7B0000

	R0 |= R1
	R0 |= 0x44000000

	return R0
}*/

func trans(input []byte) []byte {
	var BB [16]uint32
	salt := [4]uint32{0x7BA2E3FC, 0xE381CF0C, 0x0C1C991C, 0xACC7E8E4}
	for i := 0; i < 16; i++ {
		BB[i] = uint32(input[4*i+2]) | (uint32(input[4*i+1]) << 8) | (uint32(input[4*i]) << 16) | (uint32(input[4*i+3]) << 24)
	}

	R8 := salt[0] + BB[0] + (salt[1] ^ salt[2] ^ salt[3]) + 0xE964717C
	R8 = ROR(R8, 7)
	R8 += salt[1]

	R9 := salt[1] ^ salt[2]
	R9 ^= R8

	RA := salt[3] + BB[1]
	R9 += RA
	R9 += 0x4C1679EB
	R9 = ROR(R9, 12)
	R9 += R8

	RB := salt[2] + BB[2]
	RC := R9 ^ R8
	RD := RC ^ salt[1]
	RB += RD
	RB += 0xF2276156
	RB = ROR(RB, 17)
	RB += R9

	RC ^= RB
	RD = salt[1] + BB[3]
	RC += RD
	RC += 0x2165DCC8
	RC = ROR(RC, 22)

	R8 += BB[4]
	RC += RB
	RE := RC ^ RB
	RF := RE ^ R9
	R8 += RF
	R8 += 0x3374FEEF
	R8 = ROR(R8, 7)

	R9 += BB[5]
	R8 += RC
	RE ^= R8
	R9 += RE
	R9 += 0x57C91AB7
	R9 = ROR(R9, 12)

	RB += BB[6]
	R9 += R8
	RE = R9 ^ R8
	RF = RE ^ RC
	RB += RF
	RB += 0x1571DDD2
	RB = ROR(RB, 17)

	RC += BB[7]
	RB += R9
	RE ^= RB
	RC += RE
	RC += 0x322D412E
	RC = ROR(RC, 22)

	R8 += BB[8]
	RC += RB
	RE = RC ^ RB
	RF = RE ^ R9
	R8 += RF
	R8 += 0xE1F9B8EE
	R8 = ROR(R8, 7)

	R9 += BB[9]
	R8 += RC
	RE ^= R8
	R9 += RE
	R9 += 0xDAD13A66
	R9 = ROR(R9, 12)

	RB += BB[10]
	R9 += R8
	RE = R8 ^ RC
	RE ^= R9
	RB += RE
	RB += 0x5F1AA56D
	RB = ROR(RB, 17)

	RC += BB[11]
	RB += R9
	RE = R9 ^ R8
	RE ^= RB
	RC += RE
	RC += 0xF534C0B8
	RC = ROR(RC, 22)

	R8 += BB[12]
	RC += RB
	RE = RB ^ R9
	RE ^= RC
	R8 += RE
	R8 += 0xAFEEFE23
	R8 = ROR(R8, 7)

	R9 += BB[13]
	R8 += RC
	RE = RC ^ RB
	RE ^= R8
	R9 += RE
	R9 += 0x197B4FAC
	R9 = ROR(R9, 12)

	RA = RB + BB[14]
	R9 += R8
	RB = R8 ^ RC
	RB ^= R9
	RA += RB
	RA += 0x6C782B7A
	RA = ROR(RA, 17)

	RB = RC + BB[15]
	RA += R9
	RC = R9 ^ R8
	RC ^= RA
	RB += RC
	RB += 0xC98050D7
	RB = ROR(RB, 22)

	//round 2
	RB += RA
	RC = RB ^ 0xFFFFFFFF
	RC &= RA
	RD = RB & R9
	RC |= RD
	R8 += BB[1]
	R8 += RC
	R8 += 0x3B9908EB
	R8 = ROR(R8, 5)

	R8 += RB
	RC = R8 ^ 0xFFFFFFFF
	RC &= RB
	RD = R8 & RA
	RC |= RD
	R9 += BB[6]
	R9 += RC
	R9 += 0x4CC61F65
	R9 = ROR(R9, 9)

	R9 += R8
	RD = R9 ^ 0xFFFFFFFF
	RD &= R8
	RE = R9 & RB
	RD |= RE
	RA += BB[11]
	RA += RD
	RA += 0x786A9C1F
	RA = ROR(RA, 14)
	RA += R9

	RE = ^RA
	RE &= R9
	RF = RA & R8
	RE |= RF
	RB += BB[0]
	RB += RE
	RB += 0xD8A49D6B
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 := RB & R9
	RF |= R10
	R8 += BB[5]
	R8 += RF
	R8 += 0x5B027470
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[10]
	R9 += RF
	R9 += 0xC924A3F8
	R9 = ROR(R9, 9)
	R9 += R8

	RF = ^R9
	RF &= R8
	R10 = R9 & RB
	RF |= R10
	RA += BB[15]
	RA += RF
	RA += 0x9006E5CA
	RA = ROR(RA, 14)
	RA += R9

	RF = ^RA
	RF &= R9
	R10 = RA & R8
	RF |= R10
	RB += BB[4]
	RB += RF
	RB += 0xD0879BFE
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 = RB & R9
	RF |= R10
	R8 += BB[9]
	R8 += RF
	R8 += 0x8B0CD1AA
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[14]
	R9 += RF
	R9 += 0xF9B68E7F
	R9 = ROR(R9, 9)
	R9 += R8

	RF = ^R9
	RF &= R8
	R10 = R9 & RB
	RF |= R10
	RA += BB[3]
	RA += RF
	RA += 0x1CBEAEB1
	RA = ROR(RA, 14)
	RA += R9

	RF = ^RA
	RF &= R9
	R10 = RA & R8
	RF |= R10
	RB += BB[8]
	RB += RF
	RB += 0xB8EF550A
	RB = ROR(RB, 20)
	RB += RA

	RF = ^RB
	RF &= RA
	R10 = RB & R9
	RF |= R10
	R8 += BB[13]
	R8 += RF
	R8 += 0xCE987C65
	R8 = ROR(R8, 5)
	R8 += RB

	RF = ^R8
	RF &= RB
	R10 = R8 & RA
	RF |= R10
	R9 += BB[2]
	R9 += RF
	R9 += 0x3DA31760
	R9 = ROR(R9, 9)
	R9 += R8

	RC = R9 & RB
	RF = ^R9
	R10 = R8 & RF
	RC |= R10
	RA += BB[7]
	RA += RC
	RA += 0x97F7C41B
	RA = ROR(RA, 14)
	RA += R9

	RC = RA & R8
	RD = ^RA
	R10 = R9 & RD
	RC |= R10
	RB += BB[12]
	RB += RC
	RB += 0x14CFCA9A
	RB = ROR(RB, 20)
	RB += RA

	//ROUND3
	RC = RB & RF
	RE = RA & R9
	RC |= RE
	R8 += BB[5]
	R8 += RC
	R8 += 0x74520A3D
	R8 = ROR(R8, 4)
	R8 += RB

	RC = R8 & RD
	RD = RB & RA
	RC |= RD
	R9 += BB[8]
	R9 += RC
	R9 += 0x1B42F71A
	R9 = ROR(R9, 11)
	R9 += R8

	RC = ^RB
	RC &= R9
	RD = R8 & RB
	RC |= RD
	RA += BB[11]
	RA += RC
	RA += 0xB6FC6B3E
	RA = ROR(RA, 16)
	RA += R9

	RD = ^R8
	RD &= RA
	RE = R9 & R8
	RD |= RE
	RB += BB[14]
	RB += RD
	RB += 0x37EBB9C8
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[1]
	R8 += RE
	R8 += 0x90D2F2FB
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[4]
	R9 += RE
	R9 += 0x8DB0ED9A
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[7]
	RA += RE
	RA += 0xF998F8C0
	RA = ROR(RA, 16)
	RA += R9

	RE = ^R8
	RE &= RA
	RF = R9 & R8
	RE |= RF
	RB += BB[10]
	RB += RE
	RB += 0x8EFD55CE
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[13]
	R8 += RE
	R8 += 0x177DA541
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[0]
	R9 += RE
	R9 += 0xF07A1E18
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[3]
	RA += RE
	RA += 0xDA8106D6
	RA = ROR(RA, 16)
	RA += R9

	RE = ^R8
	RE &= RA
	RF = R9 & R8
	RE |= RF
	RB += BB[6]
	RB += RE
	RB += 0x867F911D
	RB = ROR(RB, 23)
	RB += RA

	RE = ^R9
	RE &= RB
	RF = RA & R9
	RE |= RF
	R8 += BB[9]
	R8 += RE
	R8 += 0x1D75F5CE
	R8 = ROR(R8, 4)
	R8 += RB

	RE = ^RA
	RE &= R8
	RF = RB & RA
	RE |= RF
	R9 += BB[12]
	R9 += RE
	R9 += 0x8E9F3F29
	R9 = ROR(R9, 11)
	R9 += R8

	RE = ^RB
	RE &= R9
	RF = R8 & RB
	RE |= RF
	RA += BB[15]
	RA += RE
	RA += 0x7B947DE9
	RA = ROR(RA, 16)
	RA += R9

	RC = R8 + BB[0]

	RE = ^R8
	R8 &= R9
	RE &= RA
	R8 |= RE
	RB += BB[2]
	R8 += RB
	R8 += 0x0C1A8C6B
	R8 = ROR(R8, 23)
	R8 += RA

	RB = R9 + BB[7]

	//ROUND4
	RD = ^R8
	R9 |= RD
	R9 ^= RA
	R9 += RC
	R9 += 0xC5F38D09
	R9 = ROR(R9, 6)
	R9 += R8

	R5 := RA + BB[14]

	RD = ^R9
	RA |= RD
	RA ^= R8
	RA += RB
	RA += 0x2DDBC5E2
	RA = ROR(RA, 10)
	RA += R9

	RD = R8 + BB[5]

	RE = ^RA
	R8 |= RE
	R8 ^= R9
	R5 += R8
	R5 += 0x78DB65D3
	R5 = ROR(R5, 15)
	R5 += RA

	RE = R9 + BB[12] //?D238

	RF = ^R5
	R9 |= RF
	R9 ^= RA
	R9 += RD
	R9 += 0x067FB51C
	R9 = ROR(R9, 21)
	R9 += R5

	RF = RA + BB[3]

	R10 = ^R9
	RA |= R10
	RA ^= R5
	RA += RE
	RA += 0xCD02F764
	RA = ROR(RA, 6)
	RA += R9

	RE = R5 + BB[10] //D2C0

	R10 = ^RA
	R5 |= R10
	R5 ^= R9
	R5 += RF
	R5 += 0xDB7339C6
	R5 = ROR(R5, 10)
	R5 += RA

	R4 := R9 + BB[1]

	RF = ^R5
	R9 |= RF
	R9 ^= RA
	R9 += RE
	R9 += 0x52A06D65
	R9 = ROR(R9, 15)
	R9 += R5

	RE = RA + BB[8]

	RF = ^R9
	RA |= RF
	RA ^= R5
	R4 += RA
	R4 += 0x31EE2625
	R4 = ROR(R4, 21)
	R4 += R9

	R2 := R5 + BB[15] //D374

	RA = ^R4
	R5 |= RA
	R5 ^= R9
	R5 += RE
	R5 += 0x5679D1A0
	R5 = ROR(R5, 6)
	R5 += R4

	R7 := R9 + BB[6]

	RA = ^R5
	R9 |= RA
	R9 ^= R4
	R2 += R9
	R2 += 0xB51E7C0B
	R2 = ROR(R2, 10)
	R2 += R5

	R9 = R4 + BB[13] //D3EC

	RA = ^R2
	R4 |= RA
	R4 ^= R5
	R4 += R7
	R4 += 0x57C21954
	R4 = ROR(R4, 15)
	R4 += R2

	R3 := R5 + BB[4]

	R7 = ^R4
	R5 |= R7
	R5 ^= R2
	R5 += R9
	R5 += 0xDCBE6641
	R5 = ROR(R5, 21)
	R5 += R4

	R7 = R2 + BB[11] //D464

	R9 = ^R5
	R2 |= R9
	R2 ^= R4
	R2 += R3
	R2 += 0x10D11B46
	R2 = ROR(R2, 6)
	R2 += R5

	R3 = ^R2
	R3 |= R4
	R3 ^= R5
	R3 += R7
	R3 += 0xF0210759
	R3 = ROR(R3, 10)
	R3 += R2

	R7 = R3 + salt[0]

	R1 := R4 + BB[2]
	R4 = R5 + BB[9] //D4F4

	R6 := ^R3
	R5 |= R6
	R5 ^= R2
	R1 += R5
	R1 += 0x6271CA41
	R1 = ROR(R1, 15)
	R1 += R3

	R5 = R2 + salt[3]

	R6 = ^R1
	R2 |= R6

	R6 = R1 + salt[2]
	R1 = R1 + salt[1]

	R2 ^= R3
	R2 += R4
	R2 += 0x76F8DF47
	R2 = ROR(R2, 21)
	R1 += R2

	dd := new(bytes.Buffer)
	binary.Write(dd, binary.LittleEndian, R7)
	binary.Write(dd, binary.LittleEndian, R1)
	binary.Write(dd, binary.LittleEndian, R6)
	binary.Write(dd, binary.LittleEndian, R5)

	return dd.Bytes()
}

func CalcCRC(input []byte) uint32 {

	R1 := uint32(input[1])
	R2 := uint32(input[0])

	R2 *= 0x85
	R2 += R1

	R3 := uint32(input[2])
	R3 &= 0xff
	R1 *= 0x85
	R1 += R3
	R1 *= 0x85
	R2 &= 0xff
	R2 *= 0x85
	R2 += R3
	R2 *= 0x85

	R4 := uint32(input[3])
	R4 &= 0xff
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85

	R4 = uint32(input[4])
	R4 &= 0xFF
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85

	R4 = uint32(input[5])
	R4 &= 0xFF
	R3 += R4
	R2 += R4
	R1 *= 0x85
	R1 += R4

	R4 = uint32(input[6])
	R4 &= 0xFF
	R1 *= 0x85
	R2 *= 0x85
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4

	R4 = uint32(input[7])
	R4 &= 0xFF
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4

	R4 = uint32(input[8])
	R4 &= 0xFF
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85
	R3 += R4
	R2 += R4
	R1 *= 0x85
	R1 += R4

	R4 = uint32(input[9])
	R4 &= 0xFF
	R1 *= 0x85
	R2 *= 0x85
	R2 += R4
	R1 += R4
	R3 *= 0x85
	R3 += R4

	R4 = uint32(input[10])
	R4 &= 0xFF
	R3 *= 0x85
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 *= 0x85
	R2 += R4

	R4 = uint32(input[11])
	R4 &= 0xFF
	R2 &= 0xFF
	R2 *= 0x85
	R3 &= 0xFF
	R3 *= 0x85
	R1 *= 0x85
	R1 += R4
	R3 += R4
	R2 += R4

	R6 := uint32(input[12])
	R6 &= 0xFF

	R5 := uint32(input[13])
	R5 &= 0xFF

	R4 = uint32(input[14])
	R4 &= 0xFF

	R1 *= 0x85
	R1 += R6
	R1 &= 0xFF
	R1 *= 0x85
	R1 += R5
	R1 *= 0x85
	R1 += R4

	R1 <<= 8
	R1 &= 0x6F00

	R2 *= 0x85
	R2 += R6
	R3 *= 0x85
	R3 += R6
	R3 *= 0x85
	R3 += R5
	R2 *= 0x85
	R2 += R5
	R2 &= 0x3F
	R1 |= R2

	R2 = R3 & 0xFF
	R2 *= 0x85
	R2 += R4

	R0 := uint32(input[15])
	R2 *= 0x85
	R0 += R2
	R0 <<= 16
	R0 &= 0x7B0000

	R0 |= R1
	R0 |= 0x43000000

	return R0
}

// rqt
func CalcMsgCrcForData_7019(s []byte) uint32 {
	h := md5.Sum(s)
	//return CalcMsgCrcForString_7019(hex.EncodeToString(h[:]))
	return RQTX(hex.EncodeToString(h[:]))
}

// CalcRQTX calc in hash
// @in: input string
func CalcMsgCrcForString_7019(instr string) uint32 {
	if len(instr) != 32 {
		return 0
	}

	r := new(RQTXHASH)
	r.init()
	r.update(t1)
	var T1 [16]uint32

	input := bytes.NewBuffer([]byte(instr))
	binary.Read(input, binary.LittleEndian, &T1[0])
	binary.Read(input, binary.LittleEndian, &T1[1])
	binary.Read(input, binary.LittleEndian, &T1[2])
	binary.Read(input, binary.LittleEndian, &T1[3])
	binary.Read(input, binary.LittleEndian, &T1[4])
	binary.Read(input, binary.LittleEndian, &T1[5])
	binary.Read(input, binary.LittleEndian, &T1[6])
	binary.Read(input, binary.LittleEndian, &T1[7])
	T1[8] = 0x000000FC
	T1[9] = 0x00000000
	T1[10] = 0x00000000
	T1[11] = 0x00000000
	T1[12] = 0x00000000
	T1[13] = 0x00000000
	T1[14] = 0xFFFFFFFF
	T1[15] = 0xFFFCFFFF

	r.update(T1)
	hash1 := r.final()

	r = new(RQTXHASH)
	r.init()
	r.update(t2)

	var T2 [16]uint32
	input = bytes.NewBuffer(hash1)
	binary.Read(input, binary.LittleEndian, &T2[0])
	binary.Read(input, binary.LittleEndian, &T2[1])
	binary.Read(input, binary.LittleEndian, &T2[2])
	binary.Read(input, binary.LittleEndian, &T2[3])
	binary.Read(input, binary.LittleEndian, &T2[4])
	binary.Read(input, binary.LittleEndian, &T2[5])
	binary.Read(input, binary.LittleEndian, &T2[6])
	binary.Read(input, binary.LittleEndian, &T2[7])
	T2[8] = 0x000000FC
	T2[9] = 0x00000000
	T2[10] = 0x00000000
	T2[11] = 0x00000000
	T2[12] = 0x00000000
	T2[13] = 0x00000000
	T2[14] = 0xFFFFFFFF
	T2[15] = 0xFFFCFFFF

	r.update(T2)
	hash2 := r.final()

	FH := append(hash2,
		[]byte{
			0xDA, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0xFB, 0x0F, 0x13, 0x64, 0x00, 0x00,
			0x00, 0x00}...,
	)
	return CalcCRC(trans(FH))
}

func RQTX(in string) uint32 {
	if len(in) != 32 {
		return 0
	}
	r := new(RQTXHASH)
	r.init()
	r.update(t1)
	var T1 [16]uint32

	input := bytes.NewBuffer([]byte(in))
	binary.Read(input, binary.LittleEndian, &T1[0])
	binary.Read(input, binary.LittleEndian, &T1[1])
	binary.Read(input, binary.LittleEndian, &T1[2])
	binary.Read(input, binary.LittleEndian, &T1[3])
	binary.Read(input, binary.LittleEndian, &T1[4])
	binary.Read(input, binary.LittleEndian, &T1[5])
	binary.Read(input, binary.LittleEndian, &T1[6])
	binary.Read(input, binary.LittleEndian, &T1[7])
	T1[8] = 0x000000FC
	T1[9] = 0x00000000
	T1[10] = 0x00000000
	T1[11] = 0x00000000
	T1[12] = 0x00000000
	T1[13] = 0x00000000
	T1[14] = 0xFFFFFFFF
	T1[15] = 0xFFFCFFFF

	r.update(T1)
	hash1 := r.final()
	r = new(RQTXHASH)
	r.init()
	r.update(t2)

	var T2 [16]uint32
	input = bytes.NewBuffer(hash1)
	binary.Read(input, binary.LittleEndian, &T2[0])
	binary.Read(input, binary.LittleEndian, &T2[1])
	binary.Read(input, binary.LittleEndian, &T2[2])
	binary.Read(input, binary.LittleEndian, &T2[3])
	binary.Read(input, binary.LittleEndian, &T2[4])
	binary.Read(input, binary.LittleEndian, &T2[5])
	binary.Read(input, binary.LittleEndian, &T2[6])
	binary.Read(input, binary.LittleEndian, &T2[7])
	T2[8] = 0x000000FC
	T2[9] = 0x00000000
	T2[10] = 0x00000000
	T2[11] = 0x00000000
	T2[12] = 0x00000000
	T2[13] = 0x00000000
	T2[14] = 0xFFFFFFFF
	T2[15] = 0xFFFCFFFF

	r.update(T2)
	hash2 := r.final()

	FH := append(hash2,
		[]byte{
			0xDA, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0xFB, 0x0F, 0x13, 0x64, 0x00, 0x00,
			0x00, 0x00}...,
	)
	return CalcCRC(trans(FH))
}
