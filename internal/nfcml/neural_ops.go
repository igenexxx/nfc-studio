package nfcml

import "math"

func quantizeNFCInt8(src []float32, qDst []int8, n int) float32 {
	var maxAbs float32
	for i := range n {
		v := src[i]
		if v < 0 {
			v = -v
		}
		if v > maxAbs {
			maxAbs = v
		}
	}
	if maxAbs < 1e-8 {
		clear(qDst[:n])
		return 0
	}
	scale := maxAbs / 127.0
	for i := range n {
		r := math.Round(float64(src[i] / scale))
		if r > 127 {
			r = 127
		} else if r < -127 {
			r = -127
		}
		qDst[i] = int8(r)
	}
	return scale
}

func applyActNFC(v float32, act uint8) float32 {
	if act == actReLU6 {
		return max(float32(0), min(float32(6), v))
	}
	return v
}

func runNFCLayer1D(dst, src []float32, qBuf []int8, inLen int, l *czmlLayer) int {
	inC, outC := int(l.InChannels), int(l.OutChannels)
	sx := quantizeNFCInt8(src, qBuf, inC*inLen)
	k, stride, dil := int(l.KernelSize), int(l.Stride), int(l.Dilation)
	pad := (k / 2) * dil
	outLen := inLen / stride

	if l.OpType == opPointwiseConv1D {
		runNFCPointwise1D(dst, qBuf, sx, inC, outC, inLen, l)
		return inLen
	}
	if l.OpType == opDepthwiseConv1D {
		runNFCDepthwise1D(dst, qBuf, sx, outC, inLen, outLen, k, stride, dil, pad, l)
		return outLen
	}
	runNFCConv1D(dst, qBuf, sx, inC, outC, inLen, outLen, k, stride, dil, pad, l)
	return outLen
}

func runNFCPointwise1D(dst []float32, xq []int8, sx float32, inC, outC, inLen int, l *czmlLayer) {
	var acc [InductiveSeqLen]int32
	for o := range outC {
		wRow := l.Weights[o*inC : (o+1)*inC]
		sc, b := l.WeightScales[o]*sx, l.Biases[o]
		outRow := dst[o*inLen : (o+1)*inLen]
		clear(acc[:inLen])
		for c := range inC {
			w := int32(wRow[c])
			if w == 0 {
				continue
			}
			inRow := xq[c*inLen : (c+1)*inLen]
			for t, xv := range inRow {
				acc[t] += w * int32(xv)
			}
		}
		for t := range inLen {
			outRow[t] = applyActNFC(float32(float32(acc[t])*sc)+b, l.Activation)
		}
	}
}

func runNFCDepthwise1D(dst []float32, xq []int8, sx float32, outC, inLen, outLen, k, stride, dil, pad int, l *czmlLayer) {
	for c := range outC {
		wRow := l.Weights[c*k : (c+1)*k]
		sc, b := l.WeightScales[c]*sx, l.Biases[c]
		for to := range outLen {
			base := to*stride - pad
			var acc int32
			for ki := range k {
				if ti := base + ki*dil; ti >= 0 && ti < inLen {
					acc += int32(wRow[ki]) * int32(xq[c*inLen+ti])
				}
			}
			dst[c*outLen+to] = applyActNFC(float32(acc)*sc+b, l.Activation)
		}
	}
}

func runNFCConv1D(dst []float32, xq []int8, sx float32, inC, outC, inLen, outLen, k, stride, dil, pad int, l *czmlLayer) {
	wPerOut := inC * k
	for o := range outC {
		sc, b := l.WeightScales[o]*sx, l.Biases[o]
		for to := range outLen {
			base := to*stride - pad
			var acc int32
			for c := range inC {
				wc := o*wPerOut + c*k
				for ki := range k {
					if ti := base + ki*dil; ti >= 0 && ti < inLen {
						acc += int32(l.Weights[wc+ki]) * int32(xq[c*inLen+ti])
					}
				}
			}
			dst[o*outLen+to] = applyActNFC(float32(acc)*sc+b, l.Activation)
		}
	}
}

func runNFCLinear(dst, src []float32, qBuf []int8, l *czmlLayer) {
	inF, outF := int(l.InChannels), int(l.OutChannels)
	sx := quantizeNFCInt8(src, qBuf, inF)
	for o := range outF {
		wRow := l.Weights[o*inF : (o+1)*inF]
		var acc int32
		for i := range inF {
			acc += int32(wRow[i]) * int32(qBuf[i])
		}
		dst[o] = applyActNFC(float32(acc)*(l.WeightScales[o]*sx)+l.Biases[o], l.Activation)
	}
}

func poolAvgMaxNFC(dst, src []float32, ch, seq int) {
	inv := float32(1.0 / float64(seq))
	for c := range ch {
		row := src[c*seq : (c+1)*seq]
		sum, mx := row[0], row[0]
		for t := 1; t < seq; t++ {
			sum += row[t]
			mx = max(mx, row[t])
		}
		dst[c] = sum * inv
		dst[ch+c] = mx
	}
}

func softmaxNFC(dst, logits []float32, n int, temp float32) (int, float32) {
	t := max(float64(temp), 1e-4)
	maxL := float64(logits[0]) / t
	for i := 1; i < n; i++ {
		maxL = max(maxL, float64(logits[i])/t)
	}
	var sum float64
	for i := range n {
		e := math.Exp(float64(logits[i])/t - maxL)
		dst[i] = float32(e)
		sum += e
	}
	inv := float32(1.0 / sum)
	bestIdx, bestProb := 0, float32(0)
	for i := range n {
		dst[i] *= inv
		if dst[i] > bestProb {
			bestProb, bestIdx = dst[i], i
		}
	}
	return bestIdx, bestProb
}

func normalize32(src []float32) [LearnedEmbedDim]float32 {
	var out [LearnedEmbedDim]float32
	var ss float64
	for i := range LearnedEmbedDim {
		ss += float64(src[i]) * float64(src[i])
	}
	if ss <= 1e-12 {
		return out
	}
	inv := float32(1.0 / math.Sqrt(ss))
	for i := range LearnedEmbedDim {
		out[i] = src[i] * inv
	}
	return out
}

func clamp01(v float32) float32     { return max(float32(0), min(float32(1), v)) }
func clampSigned(v float32) float32 { return max(float32(-1), min(float32(1), v)) }
