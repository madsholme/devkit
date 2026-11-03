package main

import "strings"

// A small QR code encoder for pairing links: byte mode, error correction level M, versions 1–10
// (up to 213 bytes), following ISO/IEC 18004. devkit uses no dependencies, so it carries its own.

const qrMaxVersion = 10

// Error correction codewords per block and number of blocks at level M, by version.
var (
	qrECCPerBlock = [qrMaxVersion + 1]int{0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26}
	qrBlocks      = [qrMaxVersion + 1]int{0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5}
)

// qrCode encodes text and returns its modules, true for dark, without the quiet zone.
func qrCode(text string) ([][]bool, error) {
	data := []byte(text)
	version := 0
	for v := 1; v <= qrMaxVersion; v++ {
		if 4+qrCountBits(v)+8*len(data) <= 8*qrDataCodewords(v) {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, fail("%d bytes are too long for a QR code here.", len(data))
	}

	// Mode, length, data, terminator, then padding to the capacity.
	var bits qrBits
	bits.add(0b0100, 4)
	bits.add(len(data), qrCountBits(version))
	for _, b := range data {
		bits.add(int(b), 8)
	}
	capacity := 8 * qrDataCodewords(version)
	bits.add(0, min(4, capacity-len(bits)))
	bits.add(0, (8-len(bits)%8)%8)
	for pad := 0xEC; len(bits) < capacity; pad ^= 0xEC ^ 0x11 {
		bits.add(pad, 8)
	}

	q := newQR(version)
	q.drawData(qrInterleave(version, bits.bytes()))
	best, bestPenalty := 0, -1
	for mask := range 8 {
		q.applyMask(mask)
		q.drawFormat(mask)
		if p := q.penalty(); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = mask, p
		}
		q.applyMask(mask) // XOR again undoes it
	}
	q.applyMask(best)
	q.drawFormat(best)
	return q.modules, nil
}

func qrCountBits(version int) int {
	if version < 10 {
		return 8
	}
	return 16
}

// qrRawModules is how many modules of a version carry data or error correction.
func qrRawModules(version int) int {
	n := (16*version+128)*version + 64
	if version >= 2 {
		align := version/7 + 2
		n -= (25*align-10)*align - 55
		if version >= 7 {
			n -= 36
		}
	}
	return n
}

func qrDataCodewords(version int) int {
	return qrRawModules(version)/8 - qrECCPerBlock[version]*qrBlocks[version]
}

type qrBits []bool

func (b *qrBits) add(value, n int) {
	for i := n - 1; i >= 0; i-- {
		*b = append(*b, value>>i&1 == 1)
	}
}

func (b qrBits) bytes() []byte {
	out := make([]byte, len(b)/8)
	for i, bit := range b {
		if bit {
			out[i/8] |= 0x80 >> (i % 8)
		}
	}
	return out
}

// qrInterleave splits data into blocks, adds each block's error correction and interleaves them.
func qrInterleave(version int, data []byte) []byte {
	blocks, ecc := qrBlocks[version], qrECCPerBlock[version]
	raw := qrRawModules(version) / 8
	short := raw / blocks // codewords in a short block, error correction included
	longFrom := blocks - raw%blocks
	var dataBlocks, eccBlocks [][]byte
	for i, k := 0, 0; i < blocks; i++ {
		n := short - ecc
		if i >= longFrom {
			n++
		}
		dataBlocks = append(dataBlocks, data[k:k+n])
		eccBlocks = append(eccBlocks, qrReedSolomon(data[k:k+n], ecc))
		k += n
	}
	var out []byte
	for i := 0; i <= short-ecc; i++ {
		for _, b := range dataBlocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := range ecc {
		for _, b := range eccBlocks {
			out = append(out, b[i])
		}
	}
	return out
}

// qrMultiply multiplies in GF(2^8) with the QR code's polynomial x^8 + x^4 + x^3 + x^2 + 1.
func qrMultiply(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		z = z<<1 ^ (z>>7)*0x1D
		z ^= (y >> i & 1) * x
	}
	return z
}

// qrReedSolomon returns the n error correction codewords of data.
func qrReedSolomon(data []byte, n int) []byte {
	// The generator polynomial (x - 2^0)(x - 2^1)...(x - 2^(n-1)), leading coefficient dropped.
	generator := make([]byte, n)
	generator[n-1] = 1
	root := byte(1)
	for range n {
		for j := range generator {
			generator[j] = qrMultiply(generator[j], root)
			if j+1 < n {
				generator[j] ^= generator[j+1]
			}
		}
		root = qrMultiply(root, 2)
	}
	remainder := make([]byte, n)
	for _, b := range data {
		factor := b ^ remainder[0]
		copy(remainder, remainder[1:])
		remainder[n-1] = 0
		for i, g := range generator {
			remainder[i] ^= qrMultiply(g, factor)
		}
	}
	return remainder
}

type qrSymbol struct {
	size              int
	modules, function [][]bool
}

// newQR draws the function patterns of a version: finders, timing, alignment and version info,
// and reserves the format areas.
func newQR(version int) *qrSymbol {
	size := 4*version + 17
	q := &qrSymbol{size: size}
	for range size {
		q.modules = append(q.modules, make([]bool, size))
		q.function = append(q.function, make([]bool, size))
	}
	for i := range size {
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	for _, c := range [][2]int{{3, 3}, {size - 4, 3}, {3, size - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := c[0]+dx, c[1]+dy
				if x >= 0 && x < size && y >= 0 && y < size {
					d := max(abs(dx), abs(dy))
					q.set(x, y, d != 2 && d != 4)
				}
			}
		}
	}
	if version >= 2 {
		align := version/7 + 2
		step := (version*4 + align*2 + 1) / (align*2 - 2) * 2
		positions := make([]int, align)
		positions[0] = 6
		for i, pos := align-1, size-7; i >= 1; i, pos = i-1, pos-step {
			positions[i] = pos
		}
		for i, cx := range positions {
			for j, cy := range positions {
				if (i == 0 && j == 0) || (i == 0 && j == align-1) || (i == align-1 && j == 0) {
					continue // under a finder
				}
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						q.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
					}
				}
			}
		}
	}
	q.drawFormat(0) // reserves the format areas; the real bits come with the mask
	if version >= 7 {
		rem := version
		for range 12 {
			rem = rem<<1 ^ (rem>>11)*0x1F25
		}
		bits := version<<12 | rem
		for i := range 18 {
			dark := bits>>i&1 == 1
			a, b := size-11+i%3, i/3
			q.set(a, b, dark)
			q.set(b, a, dark)
		}
	}
	return q
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// set draws a function module at column x, row y.
func (q *qrSymbol) set(x, y int, dark bool) {
	q.modules[y][x] = dark
	q.function[y][x] = true
}

// drawFormat draws both copies of the format information for level M and a mask.
func (q *qrSymbol) drawFormat(mask int) {
	data := 0b00<<3 | mask // level M
	rem := data
	for range 10 {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return bits>>i&1 == 1 }
	for i := range 6 {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		q.set(q.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, q.size-15+i, bit(i))
	}
	q.set(8, q.size-8, true) // the dark module
}

// drawData places the codewords in the zigzag order, two columns at a time from the right.
func (q *qrSymbol) drawData(data []byte) {
	i := 0
	for right := q.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5 // skip the vertical timing pattern
		}
		for vert := range q.size {
			for j := range 2 {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = q.size - 1 - vert // upwards
				}
				if !q.function[y][x] && i < len(data)*8 {
					q.modules[y][x] = data[i/8]>>(7-i%8)&1 == 1
					i++
				}
			}
		}
	}
}

func (q *qrSymbol) applyMask(mask int) {
	for y := range q.size {
		for x := range q.size {
			var flip bool
			switch mask {
			case 0:
				flip = (x+y)%2 == 0
			case 1:
				flip = y%2 == 0
			case 2:
				flip = x%3 == 0
			case 3:
				flip = (x+y)%3 == 0
			case 4:
				flip = (x/3+y/2)%2 == 0
			case 5:
				flip = x*y%2+x*y%3 == 0
			case 6:
				flip = (x*y%2+x*y%3)%2 == 0
			case 7:
				flip = ((x+y)%2+x*y%3)%2 == 0
			}
			if flip && !q.function[y][x] {
				q.modules[y][x] = !q.modules[y][x]
			}
		}
	}
}

// penalty scores how hard the symbol is to read; the mask with the lowest score wins.
func (q *qrSymbol) penalty() int {
	score, dark := 0, 0
	line := func(get func(i int) bool) {
		var s strings.Builder
		run := 0
		for i := range q.size {
			if i > 0 && get(i) == get(i-1) {
				run++
			} else {
				run = 1
			}
			if run == 5 {
				score += 3
			} else if run > 5 {
				score++
			}
			if get(i) {
				s.WriteByte('1')
			} else {
				s.WriteByte('0')
			}
		}
		// Finder-like patterns, with light space beyond the edges.
		padded := "0000" + s.String() + "0000"
		score += 40 * (strings.Count(padded, "10111010000") + strings.Count(padded, "00001011101"))
	}
	for y := range q.size {
		line(func(x int) bool { return q.modules[y][x] })
	}
	for x := range q.size {
		line(func(y int) bool { return q.modules[y][x] })
	}
	for y := range q.size {
		for x := range q.size {
			c := q.modules[y][x]
			if c {
				dark++
			}
			if x+1 < q.size && y+1 < q.size && c == q.modules[y][x+1] && c == q.modules[y+1][x] && c == q.modules[y+1][x+1] {
				score += 3
			}
		}
	}
	total := q.size * q.size
	return score + 10*((abs(dark*20-total*10)+total-1)/total-1)
}

// qrText draws modules for a terminal, two rows per line, with a quiet zone of four modules.
func qrText(modules [][]bool) string {
	n := len(modules) + 8
	at := func(x, y int) bool {
		x, y = x-4, y-4
		return y >= 0 && y < len(modules) && x >= 0 && x < len(modules) && modules[y][x]
	}
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := range n {
			// Light modules print as blocks, so the code reads on a dark terminal.
			switch top, bottom := !at(x, y), !at(x, y+1) && y+1 < n; {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
