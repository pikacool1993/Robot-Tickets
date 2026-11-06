package baseutils

import srand "wechatdll/clientsdk/securerandom"

func RandSeq(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[srand.SecureIntn(len(letters))]
	}
	return string(b)
}
