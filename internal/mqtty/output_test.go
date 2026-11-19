package mqtty

import "testing"

func TestIncompleteUTF8Tail(t *testing.T) {
	zh := []byte("中") // 3 个字节
	for _, c := range []struct {
		in   []byte
		want int
	}{
		{[]byte("ls -la\r\n"), 0},
		{append([]byte("ab"), zh...), 0},
		{append([]byte("ab"), zh[:1]...), 1},
		{append([]byte("ab"), zh[:2]...), 2},
		{[]byte{0xF0, 0x9F, 0x98}, 3}, // 切开的 emoji
		{[]byte{0xFF}, 0},             // 本来就不是 UTF-8 的字节照常发出
		{nil, 0},
	} {
		if got := incompleteUTF8Tail(c.in); got != c.want {
			t.Errorf("incompleteUTF8Tail(% x) = %d，应为 %d", c.in, got, c.want)
		}
	}
}
