package installer

// zlibDeflate is zlib's raw deflate at the default level, byte for byte:
// zlib.compressobj(zlib.Z_DEFAULT_COMPRESSION, zlib.DEFLATED, -15) followed
// by compress(data) + flush(), the compressor CPython's zipfile uses for
// ZIP_DEFLATED. Go's compress/flate produces valid but different streams;
// the Compose bundle's content_base64 must equal Python's, so this is a port
// of zlib 1.3 deflate.c (deflate_slow, longest_match, fill_window with its
// high-water zeroing) and trees.c for level 6 (good 8, lazy 16, nice 128,
// chain 128), windowBits 15, memLevel 8, Z_DEFAULT_STRATEGY.

const (
	zMinMatch     = 3
	zMaxMatch     = 258
	zWSize        = 1 << 15
	zWMask        = zWSize - 1
	zWindowSize   = 2 * zWSize
	zHashBits     = 8 + 7 // memLevel + 7
	zHashSize     = 1 << zHashBits
	zHashMask     = zHashSize - 1
	zHashShift    = (zHashBits + zMinMatch - 1) / zMinMatch
	zMinLookahead = zMaxMatch + zMinMatch + 1
	zMaxDist      = zWSize - zMinLookahead
	zWinInit      = zMaxMatch
	zTooFar       = 4096
	zLitBufsize   = 1 << (8 + 6)
	zSymEnd       = (zLitBufsize - 1) * 3

	zGoodMatch = 8
	zMaxLazy   = 16
	zNiceMatch = 128
	zMaxChain  = 128

	zLengthCodes = 29
	zLiterals    = 256
	zLCodes      = zLiterals + 1 + zLengthCodes
	zDCodes      = 30
	zBLCodes     = 19
	zHeapSize    = 2*zLCodes + 1
	zMaxBits     = 15
	zMaxBLBits   = 7
	zEndBlock    = 256
	zRep3_6      = 16
	zRepz3_10    = 17
	zRepz11_138  = 18
)

var (
	zExtraLBits  = [zLengthCodes]int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	zExtraDBits  = [zDCodes]int{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
	zExtraBLBits = [zBLCodes]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 3, 7}
	zBLOrder     = [zBLCodes]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

	zLengthCode [zMaxMatch - zMinMatch + 1]uint8
	zDistCode   [512]uint8
	zBaseLength [zLengthCodes]int
	zBaseDist   [zDCodes]int
	zStaticL    [zLCodes + 2]zctData
	zStaticD    [zDCodes]zctData
)

// zctData is ct_data: fc is Freq/Code, dl is Dad/Len (C unions).
type zctData struct{ fc, dl uint16 }

type zStaticDesc struct {
	static    []zctData
	extra     []int
	extraBase int
	elems     int
	maxLength int
}

var (
	zStaticLDesc  = zStaticDesc{zStaticL[:], zExtraLBits[:], zLiterals + 1, zLCodes, zMaxBits}
	zStaticDDesc  = zStaticDesc{zStaticD[:], zExtraDBits[:], 0, zDCodes, zMaxBits}
	zStaticBLDesc = zStaticDesc{nil, zExtraBLBits[:], 0, zBLCodes, zMaxBLBits}
)

func init() {
	// tr_static_init
	length := 0
	code := 0
	for code = 0; code < zLengthCodes-1; code++ {
		zBaseLength[code] = length
		for n := 0; n < 1<<zExtraLBits[code]; n++ {
			zLengthCode[length] = uint8(code)
			length++
		}
	}
	zLengthCode[length-1] = uint8(code)
	dist := 0
	for code = 0; code < 16; code++ {
		zBaseDist[code] = dist
		for n := 0; n < 1<<zExtraDBits[code]; n++ {
			zDistCode[dist] = uint8(code)
			dist++
		}
	}
	dist >>= 7
	for ; code < zDCodes; code++ {
		zBaseDist[code] = dist << 7
		for n := 0; n < 1<<(zExtraDBits[code]-7); n++ {
			zDistCode[256+dist] = uint8(code)
			dist++
		}
	}
	var blCount [zMaxBits + 1]uint16
	n := 0
	for ; n <= 143; n++ {
		zStaticL[n].dl = 8
		blCount[8]++
	}
	for ; n <= 255; n++ {
		zStaticL[n].dl = 9
		blCount[9]++
	}
	for ; n <= 279; n++ {
		zStaticL[n].dl = 7
		blCount[7]++
	}
	for ; n <= 287; n++ {
		zStaticL[n].dl = 8
		blCount[8]++
	}
	zGenCodes(zStaticL[:], zLCodes+1, blCount[:])
	for n := 0; n < zDCodes; n++ {
		zStaticD[n].dl = 5
		zStaticD[n].fc = uint16(zBiReverse(uint(n), 5))
	}
}

func zBiReverse(code uint, length int) uint {
	var res uint
	for {
		res |= code & 1
		code >>= 1
		res <<= 1
		length--
		if length <= 0 {
			break
		}
	}
	return res >> 1
}

func zGenCodes(tree []zctData, maxCode int, blCount []uint16) {
	var nextCode [zMaxBits + 1]uint16
	code := uint(0)
	for bits := 1; bits <= zMaxBits; bits++ {
		code = (code + uint(blCount[bits-1])) << 1
		nextCode[bits] = uint16(code)
	}
	for n := 0; n <= maxCode; n++ {
		length := int(tree[n].dl)
		if length == 0 {
			continue
		}
		tree[n].fc = uint16(zBiReverse(uint(nextCode[length]), length))
		nextCode[length]++
	}
}

func zDCode(dist int) int {
	if dist < 256 {
		return int(zDistCode[dist])
	}
	return int(zDistCode[256+(dist>>7)])
}

type zTreeDesc struct {
	dyn     []zctData
	maxCode int
	stat    *zStaticDesc
}

type deflater struct {
	input []byte
	inPos int
	out   []byte
	biBuf uint64
	biCnt uint

	window     [zWindowSize]byte
	prev       [zWSize]uint16
	head       [zHashSize]uint16
	highWater  int
	insH       uint
	strstart   int
	blockStart int
	lookahead  int
	insert     int

	matchLength    int
	prevMatch      int
	matchAvailable bool
	matchStart     int
	prevLength     int
	dynL           [zHeapSize]zctData
	dynD           [2*zDCodes + 1]zctData
	blTree         [2*zBLCodes + 1]zctData
	lDesc, dDesc   zTreeDesc
	blDesc         zTreeDesc
	blCount        [zMaxBits + 1]uint16
	heap           [2*zLCodes + 1]int
	heapLen        int
	heapMax        int
	depth          [2*zLCodes + 1]uint8
	sym            []byte
	optLen         int64
	staticLen      int64
}

func zlibDeflate(data []byte) []byte {
	s := &deflater{input: data}
	s.lDesc = zTreeDesc{dyn: s.dynL[:], stat: &zStaticLDesc}
	s.dDesc = zTreeDesc{dyn: s.dynD[:], stat: &zStaticDDesc}
	s.blDesc = zTreeDesc{dyn: s.blTree[:], stat: &zStaticBLDesc}
	s.sym = make([]byte, 0, zSymEnd)
	s.initBlock()
	s.matchLength, s.prevLength = zMinMatch-1, zMinMatch-1
	s.deflateSlow()
	return s.out
}

func (s *deflater) sendBits(value uint, length int) {
	s.biBuf |= uint64(value) << s.biCnt
	s.biCnt += uint(length)
	for s.biCnt >= 8 {
		s.out = append(s.out, byte(s.biBuf))
		s.biBuf >>= 8
		s.biCnt -= 8
	}
}

func (s *deflater) biWindup() {
	if s.biCnt > 0 {
		s.out = append(s.out, byte(s.biBuf))
	}
	s.biBuf, s.biCnt = 0, 0
}

func (s *deflater) updateHash(c byte) {
	s.insH = ((s.insH << zHashShift) ^ uint(c)) & zHashMask
}

// insertString is INSERT_STRING: returns the previous head of the chain.
func (s *deflater) insertString(str int) int {
	s.updateHash(s.window[str+zMinMatch-1])
	head := s.head[s.insH]
	s.prev[str&zWMask] = head
	s.head[s.insH] = uint16(str)
	return int(head)
}

func (s *deflater) slideHash() {
	for i := range s.head {
		if m := int(s.head[i]); m >= zWSize {
			s.head[i] = uint16(m - zWSize)
		} else {
			s.head[i] = 0
		}
	}
	for i := range s.prev {
		if m := int(s.prev[i]); m >= zWSize {
			s.prev[i] = uint16(m - zWSize)
		} else {
			s.prev[i] = 0
		}
	}
}

func (s *deflater) fillWindow() {
	for {
		more := zWindowSize - s.lookahead - s.strstart
		if s.strstart >= zWSize+zMaxDist {
			copy(s.window[:zWSize-more], s.window[zWSize:zWSize+zWSize-more])
			s.matchStart -= zWSize
			s.strstart -= zWSize
			s.blockStart -= zWSize
			if s.insert > s.strstart {
				s.insert = s.strstart
			}
			s.slideHash()
			more += zWSize
		}
		if s.inPos == len(s.input) {
			break
		}
		n := copy(s.window[s.strstart+s.lookahead:s.strstart+s.lookahead+more], s.input[s.inPos:])
		s.inPos += n
		s.lookahead += n
		if s.lookahead+s.insert >= zMinMatch {
			str := s.strstart - s.insert
			s.insH = uint(s.window[str])
			s.updateHash(s.window[str+1])
			for s.insert > 0 {
				s.updateHash(s.window[str+zMinMatch-1])
				s.prev[str&zWMask] = s.head[s.insH]
				s.head[s.insH] = uint16(str)
				str++
				s.insert--
				if s.lookahead+s.insert < zMinMatch {
					break
				}
			}
		}
		if !(s.lookahead < zMinLookahead && s.inPos < len(s.input)) {
			break
		}
	}
	if s.highWater < zWindowSize {
		curr := s.strstart + s.lookahead
		if s.highWater < curr {
			init := zWindowSize - curr
			if init > zWinInit {
				init = zWinInit
			}
			clear(s.window[curr : curr+init])
			s.highWater = curr + init
		} else if s.highWater < curr+zWinInit {
			init := curr + zWinInit - s.highWater
			if init > zWindowSize-s.highWater {
				init = zWindowSize - s.highWater
			}
			clear(s.window[s.highWater : s.highWater+init])
			s.highWater += init
		}
	}
}

func (s *deflater) longestMatch(curMatch int) int {
	chainLength := zMaxChain
	scan := s.strstart
	bestLen := s.prevLength
	niceMatch := zNiceMatch
	limit := 0
	if s.strstart > zMaxDist {
		limit = s.strstart - zMaxDist
	}
	strend := s.strstart + zMaxMatch
	w := &s.window
	scanEnd1 := w[scan+bestLen-1]
	scanEnd := w[scan+bestLen]
	if s.prevLength >= zGoodMatch {
		chainLength >>= 2
	}
	if niceMatch > s.lookahead {
		niceMatch = s.lookahead
	}
	for {
		match := curMatch
		if !(w[match+bestLen] != scanEnd || w[match+bestLen-1] != scanEnd1 || w[match] != w[scan] || w[match+1] != w[scan+1]) {
			// scan[2] and match[2] are equal when the hash keys are (zlib skips them).
			sp, mp := scan+2, match+2
			for {
				sp++
				mp++
				if w[sp] != w[mp] || sp >= strend {
					break
				}
			}
			length := zMaxMatch - (strend - sp)
			if length > bestLen {
				s.matchStart = curMatch
				bestLen = length
				if length >= niceMatch {
					break
				}
				scanEnd1 = w[scan+bestLen-1]
				scanEnd = w[scan+bestLen]
			}
		}
		curMatch = int(s.prev[curMatch&zWMask])
		if curMatch <= limit {
			break
		}
		chainLength--
		if chainLength == 0 {
			break
		}
	}
	if bestLen <= s.lookahead {
		return bestLen
	}
	return s.lookahead
}

func (s *deflater) tallyLit(c byte) bool {
	s.sym = append(s.sym, 0, 0, c)
	s.dynL[c].fc++
	return len(s.sym) == zSymEnd
}

func (s *deflater) tallyDist(distance, length int) bool {
	s.sym = append(s.sym, byte(distance), byte(distance>>8), byte(length))
	distance--
	s.dynL[int(zLengthCode[length])+zLiterals+1].fc++
	s.dynD[zDCode(distance)].fc++
	return len(s.sym) == zSymEnd
}

func (s *deflater) flushBlock(last bool) {
	if s.blockStart >= 0 {
		s.trFlushBlock(s.window[s.blockStart:], s.strstart-s.blockStart, last)
	} else {
		s.trFlushBlock(nil, s.strstart-s.blockStart, last)
	}
	s.blockStart = s.strstart
}

func (s *deflater) deflateSlow() {
	for {
		if s.lookahead < zMinLookahead {
			s.fillWindow()
			if s.lookahead == 0 {
				break
			}
		}
		hashHead := 0
		if s.lookahead >= zMinMatch {
			hashHead = s.insertString(s.strstart)
		}
		s.prevLength, s.prevMatch = s.matchLength, s.matchStart
		s.matchLength = zMinMatch - 1
		if hashHead != 0 && s.prevLength < zMaxLazy && s.strstart-hashHead <= zMaxDist {
			s.matchLength = s.longestMatch(hashHead)
			if s.matchLength <= 5 && s.matchLength == zMinMatch && s.strstart-s.matchStart > zTooFar {
				s.matchLength = zMinMatch - 1
			}
		}
		if s.prevLength >= zMinMatch && s.matchLength <= s.prevLength {
			maxInsert := s.strstart + s.lookahead - zMinMatch
			bflush := s.tallyDist(s.strstart-1-s.prevMatch, s.prevLength-zMinMatch)
			s.lookahead -= s.prevLength - 1
			s.prevLength -= 2
			for {
				s.strstart++
				if s.strstart <= maxInsert {
					s.insertString(s.strstart)
				}
				s.prevLength--
				if s.prevLength == 0 {
					break
				}
			}
			s.matchAvailable = false
			s.matchLength = zMinMatch - 1
			s.strstart++
			if bflush {
				s.flushBlock(false)
			}
		} else if s.matchAvailable {
			if s.tallyLit(s.window[s.strstart-1]) {
				s.flushBlock(false)
			}
			s.strstart++
			s.lookahead--
		} else {
			s.matchAvailable = true
			s.strstart++
			s.lookahead--
		}
	}
	if s.matchAvailable {
		s.tallyLit(s.window[s.strstart-1])
		s.matchAvailable = false
	}
	s.insert = min(s.strstart, zMinMatch-1)
	s.flushBlock(true)
}

func (s *deflater) initBlock() {
	for n := 0; n < zLCodes; n++ {
		s.dynL[n].fc = 0
	}
	for n := 0; n < zDCodes; n++ {
		s.dynD[n].fc = 0
	}
	for n := 0; n < zBLCodes; n++ {
		s.blTree[n].fc = 0
	}
	s.dynL[zEndBlock].fc = 1
	s.optLen, s.staticLen = 0, 0
	s.sym = s.sym[:0]
}

func (s *deflater) smaller(tree []zctData, n, m int) bool {
	return tree[n].fc < tree[m].fc || (tree[n].fc == tree[m].fc && s.depth[n] <= s.depth[m])
}

func (s *deflater) pqdownheap(tree []zctData, k int) {
	v := s.heap[k]
	j := k << 1
	for j <= s.heapLen {
		if j < s.heapLen && s.smaller(tree, s.heap[j+1], s.heap[j]) {
			j++
		}
		if s.smaller(tree, v, s.heap[j]) {
			break
		}
		s.heap[k] = s.heap[j]
		k = j
		j <<= 1
	}
	s.heap[k] = v
}

func (s *deflater) genBitlen(desc *zTreeDesc) {
	tree := desc.dyn
	maxCode := desc.maxCode
	stree := desc.stat.static
	extra := desc.stat.extra
	base := desc.stat.extraBase
	maxLength := desc.stat.maxLength
	overflow := 0
	for bits := 0; bits <= zMaxBits; bits++ {
		s.blCount[bits] = 0
	}
	tree[s.heap[s.heapMax]].dl = 0
	h := s.heapMax + 1
	for ; h < zHeapSize; h++ {
		n := s.heap[h]
		bits := int(tree[tree[n].dl].dl) + 1
		if bits > maxLength {
			bits = maxLength
			overflow++
		}
		tree[n].dl = uint16(bits)
		if n > maxCode {
			continue
		}
		s.blCount[bits]++
		xbits := 0
		if n >= base {
			xbits = extra[n-base]
		}
		f := int64(tree[n].fc)
		s.optLen += f * int64(bits+xbits)
		if stree != nil {
			s.staticLen += f * int64(int(stree[n].dl)+xbits)
		}
	}
	if overflow == 0 {
		return
	}
	for {
		bits := maxLength - 1
		for s.blCount[bits] == 0 {
			bits--
		}
		s.blCount[bits]--
		s.blCount[bits+1] += 2
		s.blCount[maxLength]--
		overflow -= 2
		if overflow <= 0 {
			break
		}
	}
	for bits := maxLength; bits != 0; bits-- {
		n := int(s.blCount[bits])
		for n != 0 {
			h--
			m := s.heap[h]
			if m > maxCode {
				continue
			}
			if int(tree[m].dl) != bits {
				s.optLen += (int64(bits) - int64(tree[m].dl)) * int64(tree[m].fc)
				tree[m].dl = uint16(bits)
			}
			n--
		}
	}
}

func (s *deflater) buildTree(desc *zTreeDesc) {
	tree := desc.dyn
	stree := desc.stat.static
	elems := desc.stat.elems
	maxCode := -1
	s.heapLen, s.heapMax = 0, zHeapSize
	for n := 0; n < elems; n++ {
		if tree[n].fc != 0 {
			s.heapLen++
			s.heap[s.heapLen] = n
			maxCode = n
			s.depth[n] = 0
		} else {
			tree[n].dl = 0
		}
	}
	for s.heapLen < 2 {
		node := 0
		if maxCode < 2 {
			maxCode++
			node = maxCode
		}
		s.heapLen++
		s.heap[s.heapLen] = node
		tree[node].fc = 1
		s.depth[node] = 0
		s.optLen--
		if stree != nil {
			s.staticLen -= int64(stree[node].dl)
		}
	}
	desc.maxCode = maxCode
	for n := s.heapLen / 2; n >= 1; n-- {
		s.pqdownheap(tree, n)
	}
	node := elems
	for {
		n := s.heap[1]
		s.heap[1] = s.heap[s.heapLen]
		s.heapLen--
		s.pqdownheap(tree, 1)
		m := s.heap[1]
		s.heapMax--
		s.heap[s.heapMax] = n
		s.heapMax--
		s.heap[s.heapMax] = m
		tree[node].fc = tree[n].fc + tree[m].fc
		dn, dm := s.depth[n], s.depth[m]
		if dn >= dm {
			s.depth[node] = dn + 1
		} else {
			s.depth[node] = dm + 1
		}
		tree[n].dl, tree[m].dl = uint16(node), uint16(node)
		s.heap[1] = node
		node++
		s.pqdownheap(tree, 1)
		if s.heapLen < 2 {
			break
		}
	}
	s.heapMax--
	s.heap[s.heapMax] = s.heap[1]
	s.genBitlen(desc)
	zGenCodes(tree, maxCode, s.blCount[:])
}

func (s *deflater) scanTree(tree []zctData, maxCode int) {
	prevlen, nextlen, count := -1, int(tree[0].dl), 0
	maxCount, minCount := 7, 4
	if nextlen == 0 {
		maxCount, minCount = 138, 3
	}
	tree[maxCode+1].dl = 0xffff
	for n := 0; n <= maxCode; n++ {
		curlen := nextlen
		nextlen = int(tree[n+1].dl)
		count++
		if count < maxCount && curlen == nextlen {
			continue
		} else if count < minCount {
			s.blTree[curlen].fc += uint16(count)
		} else if curlen != 0 {
			if curlen != prevlen {
				s.blTree[curlen].fc++
			}
			s.blTree[zRep3_6].fc++
		} else if count <= 10 {
			s.blTree[zRepz3_10].fc++
		} else {
			s.blTree[zRepz11_138].fc++
		}
		count, prevlen = 0, curlen
		if nextlen == 0 {
			maxCount, minCount = 138, 3
		} else if curlen == nextlen {
			maxCount, minCount = 6, 3
		} else {
			maxCount, minCount = 7, 4
		}
	}
}

func (s *deflater) sendCode(c int, tree []zctData) { s.sendBits(uint(tree[c].fc), int(tree[c].dl)) }

func (s *deflater) sendTree(tree []zctData, maxCode int) {
	prevlen, nextlen, count := -1, int(tree[0].dl), 0
	maxCount, minCount := 7, 4
	if nextlen == 0 {
		maxCount, minCount = 138, 3
	}
	for n := 0; n <= maxCode; n++ {
		curlen := nextlen
		nextlen = int(tree[n+1].dl)
		count++
		if count < maxCount && curlen == nextlen {
			continue
		} else if count < minCount {
			for {
				s.sendCode(curlen, s.blTree[:])
				count--
				if count == 0 {
					break
				}
			}
		} else if curlen != 0 {
			if curlen != prevlen {
				s.sendCode(curlen, s.blTree[:])
				count--
			}
			s.sendCode(zRep3_6, s.blTree[:])
			s.sendBits(uint(count-3), 2)
		} else if count <= 10 {
			s.sendCode(zRepz3_10, s.blTree[:])
			s.sendBits(uint(count-3), 3)
		} else {
			s.sendCode(zRepz11_138, s.blTree[:])
			s.sendBits(uint(count-11), 7)
		}
		count, prevlen = 0, curlen
		if nextlen == 0 {
			maxCount, minCount = 138, 3
		} else if curlen == nextlen {
			maxCount, minCount = 6, 3
		} else {
			maxCount, minCount = 7, 4
		}
	}
}

func (s *deflater) buildBLTree() int {
	s.scanTree(s.dynL[:], s.lDesc.maxCode)
	s.scanTree(s.dynD[:], s.dDesc.maxCode)
	s.buildTree(&s.blDesc)
	maxBLIndex := zBLCodes - 1
	for ; maxBLIndex >= 3; maxBLIndex-- {
		if s.blTree[zBLOrder[maxBLIndex]].dl != 0 {
			break
		}
	}
	s.optLen += 3*(int64(maxBLIndex)+1) + 5 + 5 + 4
	return maxBLIndex
}

func (s *deflater) sendAllTrees(lcodes, dcodes, blcodes int) {
	s.sendBits(uint(lcodes-257), 5)
	s.sendBits(uint(dcodes-1), 5)
	s.sendBits(uint(blcodes-4), 4)
	for rank := 0; rank < blcodes; rank++ {
		s.sendBits(uint(s.blTree[zBLOrder[rank]].dl), 3)
	}
	s.sendTree(s.dynL[:], lcodes-1)
	s.sendTree(s.dynD[:], dcodes-1)
}

func (s *deflater) compressBlock(ltree, dtree []zctData) {
	for sx := 0; sx < len(s.sym); sx += 3 {
		dist := int(s.sym[sx]) | int(s.sym[sx+1])<<8
		lc := int(s.sym[sx+2])
		if dist == 0 {
			s.sendCode(lc, ltree)
			continue
		}
		code := int(zLengthCode[lc])
		s.sendCode(code+zLiterals+1, ltree)
		if extra := zExtraLBits[code]; extra != 0 {
			s.sendBits(uint(lc-zBaseLength[code]), extra)
		}
		dist--
		code = zDCode(dist)
		s.sendCode(code, dtree)
		if extra := zExtraDBits[code]; extra != 0 {
			s.sendBits(uint(dist-zBaseDist[code]), extra)
		}
	}
	s.sendCode(zEndBlock, ltree)
}

func (s *deflater) trFlushBlock(buf []byte, storedLen int, last bool) {
	lastBit := uint(0)
	if last {
		lastBit = 1
	}
	s.buildTree(&s.lDesc)
	s.buildTree(&s.dDesc)
	maxBLIndex := s.buildBLTree()
	optLenb := (s.optLen + 3 + 7) >> 3
	staticLenb := (s.staticLen + 3 + 7) >> 3
	if staticLenb <= optLenb {
		optLenb = staticLenb
	}
	switch {
	case int64(storedLen)+4 <= optLenb && buf != nil:
		// _tr_stored_block
		s.sendBits(lastBit, 3)
		s.biWindup()
		s.out = append(s.out, byte(storedLen), byte(storedLen>>8), byte(^storedLen), byte(^storedLen>>8))
		s.out = append(s.out, buf[:storedLen]...)
	case staticLenb == optLenb:
		s.sendBits(2+lastBit, 3)
		s.compressBlock(zStaticL[:], zStaticD[:])
	default:
		s.sendBits(4+lastBit, 3)
		s.sendAllTrees(s.lDesc.maxCode+1, s.dDesc.maxCode+1, maxBLIndex+1)
		s.compressBlock(s.dynL[:], s.dynD[:])
	}
	s.initBlock()
	if last {
		s.biWindup()
	}
}
