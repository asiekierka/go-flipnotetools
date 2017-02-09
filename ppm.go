package flipnotetools

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
	"time"
	"unicode/utf16"
)

var ThumbnailPalette = []color.Color{
	color.RGBA{255, 255, 255, 255},
	color.RGBA{82, 82, 82, 255},
	color.RGBA{255, 255, 255, 255},
	color.RGBA{165, 165, 165, 255},
	color.RGBA{255, 0, 0, 255},
	color.RGBA{127, 0, 0, 255},
	color.RGBA{255, 127, 127, 255},
	color.RGBA{0, 255, 0, 255},
	color.RGBA{0, 0, 255, 255},
	color.RGBA{0, 0, 127, 255},
	color.RGBA{127, 127, 255, 255},
	color.RGBA{0, 255, 0, 255},
	color.RGBA{255, 0, 255, 255},
	color.RGBA{0, 255, 0, 255},
	color.RGBA{0, 255, 0, 255},
	color.RGBA{0, 255, 0, 255},
}

var AnimationPalette = []color.Color{
	color.RGBA{0, 0, 0, 255},
	color.RGBA{255, 255, 255, 255},
	color.RGBA{255, 0, 0, 255},
	color.RGBA{0, 0, 255, 255},
}

var imaIndexTable = []int{
	-1, -1, -1, -1, 2, 4, 6, 8,
	-1, -1, -1, -1, 2, 4, 6, 8,
}

var imaStepTable = []int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17,
	19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
	50, 55, 60, 66, 73, 80, 88, 97, 107, 118,
	130, 143, 157, 173, 190, 209, 230, 253, 279, 307,
	337, 371, 408, 449, 494, 544, 598, 658, 724, 796,
	876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066,
	2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358,
	5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899,
	15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767,
}

var speedTable = []float64{
	1.0 / 30.0250310083,
	1.0 / 20.1941106698, // unsure about this one
	1.0 / 12.0173521182,
	1.0 / 6.0085185544,
	1.0 / 4.0057530563,
	1.0 / 2.0027981429, // a bit unsure about this one
	1.0 / 1.0014343461,
	1.0 / 0.5007253346,
}

const audioFrequency float64 = 8184.0

type Frame struct {
	data  [2][192][256]bool
	flags byte
	Sound [4]bool
}

type Sound []byte

type Flipnote struct {
	Frames []Frame
	Sounds [4]Sound

	FrameSpeed int
	SoundSpeed int

	Locked bool

	CreatorName    string
	LastEditorName string
	UserName       string

	Filename         string
	OriginalFilename string

	CreatorId        uint64
	PreviousEditorId uint64
	LastEditorId     uint64

	Date time.Time

	PreviewImage [64][48]byte
}

func (f *Frame) Image() image.Image {
	img := image.NewPaletted(image.Rect(0, 0, 256, 192), AnimationPalette)

	for layerLine := 0; layerLine < 192; layerLine++ {
		for layerPos := 0; layerPos < 256; layerPos++ {
			img.SetColorIndex(layerPos, layerLine, uint8(f.flags&1))
		}
	}

	for layerId := 1; layerId >= 0; layerId-- {
		layerDrawMode := uint8((f.flags >> uint(1+layerId*2)) & 3)
		if layerDrawMode == 1 {
			layerDrawMode = uint8((f.flags & 1) ^ 1)
		} else if layerDrawMode == 0 {
			layerDrawMode = uint8((f.flags & 1))
		}

		for layerLine := 0; layerLine < 192; layerLine++ {
			for layerPos := 0; layerPos < 256; layerPos++ {
				if f.data[layerId][layerLine][layerPos] {
					img.SetColorIndex(layerPos, layerLine, layerDrawMode)
				}
			}
		}
	}

	return img
}

func (f *Flipnote) SoundDuration() float64 {
	return speedTable[f.SoundSpeed]
}

func (f *Flipnote) FrameDuration() float64 {
	return speedTable[f.FrameSpeed]
}

func (f *Flipnote) SoundAsPCM(id int) ([]int16, int) {
	sound := f.Sounds[id]
	out := make([]int16, len(sound)*2)

	predictor := 0
	stepIndex := 0
	step := imaStepTable[stepIndex]

	for i := 0; i < len(sound); i++ {
		val := sound[i]
		for j := 0; j < 2; j++ {
			v := val & 0x0F

			stepIndex += imaIndexTable[v]
			if stepIndex < 0 {
				stepIndex = 0
			} else if stepIndex >= len(imaStepTable) {
				stepIndex = len(imaStepTable) - 1
			}

			diff := step >> 3
			if (v & 4) != 0 {
				diff += step
			}
			if (v & 2) != 0 {
				diff += step >> 1
			}
			if (v & 1) != 0 {
				diff += step >> 2
			}
			if (v & 8) != 0 {
				predictor -= diff
			} else {
				predictor += diff
			}
			if predictor < -32768 {
				predictor = -32768
			} else if predictor > 32767 {
				predictor = 32767
			}

			step = imaStepTable[stepIndex]

			out[i*2+j] = int16(predictor)
			val >>= 4
		}
	}

	return out, int(audioFrequency * (speedTable[f.SoundSpeed] / speedTable[f.FrameSpeed]))
}

func (f *Flipnote) MixedSoundAsPCM() ([]int16, int) {
	var soundData [4][]int16
	var frequency int

	for i := 0; i < 4; i++ {
		soundData[i], frequency = f.SoundAsPCM(i)
	}

	outSoundData := make([]int16, int(float64(frequency*len(f.Frames))*speedTable[f.FrameSpeed]))

	pos := float64(0)
	for i := range f.Frames {
		soundStartPos := int(float64(frequency) * pos)
		soundStartLeft := len(outSoundData) - soundStartPos

		for j := 0; j < 4; j++ {
			if f.Frames[i].Sound[j] && (len(soundData[j]) > 0) {
				soundPosMax := len(soundData[j])
				if (j == 0) && (soundStartLeft > soundPosMax) {
					soundPosMax = soundStartLeft
				}
				for soundPos := 0; soundPos < soundPosMax; soundPos++ {
					if (soundStartPos + soundPos) == len(outSoundData) {
						outSoundData = append(outSoundData, soundData[j][soundPos%len(soundData[j])])
						break
					} else {
						outSoundData[soundStartPos+soundPos] += soundData[j][soundPos%len(soundData[j])]
					}
				}
			}
		}

		pos += speedTable[f.FrameSpeed]
	}

	return outSoundData, frequency
}

func utfSliceToString(b []byte) string {
	bu := make([]uint16, len(b)>>1)
	for i := range bu {
		bu[i] = binary.LittleEndian.Uint16(b[(i * 2):((i + 1) * 2)])
	}
	return string(utf16.Decode(bu))
}

func readSoundData(flipnote *Flipnote, soundData []byte) error {
	for i := range flipnote.Frames {
		flipnote.Frames[i].Sound[0] = (i == 0)
		flipnote.Frames[i].Sound[1] = (soundData[i] & 0x01) != 0
		flipnote.Frames[i].Sound[2] = (soundData[i] & 0x02) != 0
		flipnote.Frames[i].Sound[3] = (soundData[i] & 0x04) != 0
	}

	i := len(flipnote.Frames)
	i += (4 - (i & 3))

	sizeSounds := [4]int{
		int(binary.LittleEndian.Uint32(soundData[(i + 0):(i + 4)])),
		int(binary.LittleEndian.Uint32(soundData[(i + 4):(i + 8)])),
		int(binary.LittleEndian.Uint32(soundData[(i + 8):(i + 12)])),
		int(binary.LittleEndian.Uint32(soundData[(i + 12):(i + 16)])),
	}

	flipnote.FrameSpeed = int(soundData[i+16])
	flipnote.SoundSpeed = int(soundData[i+17])

	i = i + 32

	for si := range flipnote.Sounds {
		flipnote.Sounds[si] = soundData[i:(i + sizeSounds[si])]
		i = i + sizeSounds[si]
	}

	return nil
}

func readAnimationData(flipnote *Flipnote, animationData []byte) error {
	oEnd := uint32(8 + binary.LittleEndian.Uint16(animationData[0:2]))

	for frameId := range flipnote.Frames {
		oPos := 8 + (frameId * 4)
		frameData := animationData[oEnd+binary.LittleEndian.Uint32(animationData[oPos:(oPos+4)]):]

		frameFlags := int(frameData[0])
		flipnote.Frames[frameId].flags = frameData[0]

		iBase := 1

		layerData := &(flipnote.Frames[frameId].data)
		if ((frameFlags & 128) == 0) && (frameId > 0) {
			offsetX := int(0)
			offsetY := int(0)

			if (frameFlags & 64) != 0 {
				if frameData[iBase] < 128 {
					offsetX += int(frameData[iBase])
				} else {
					offsetX -= 0xFF ^ int(frameData[iBase]) + 1
				}
				iBase += 1
				if frameData[iBase] < 128 {
					offsetY += int(frameData[iBase])
				} else {
					offsetY -= 0xFF ^ int(frameData[iBase]) + 1
				}
				iBase += 1
			}

			for j := range layerData {
				for k := range layerData[j] {
					for l := range layerData[j][k] {
						if k+offsetY >= 0 && k+offsetY < 192 && l+offsetX >= 0 && l+offsetX < 256 {
							layerData[j][k+offsetY][l+offsetX] = flipnote.Frames[frameId-1].data[j][k][l]
						}
					}
				}
			}
		}

		i := iBase + 96

		for layerId := 0; layerId < 2; layerId++ {
			for layerLine := 0; layerLine < 192; layerLine++ {
				layerDecodeMode := (frameData[iBase+layerId*48+(layerLine>>2)] >> uint((layerLine&3)*2)) & 3

				switch layerDecodeMode {
				case 0:
					break
				case 1, 2: // coded/inv-coded line
					layerUsedBytes := binary.BigEndian.Uint32(frameData[i : i+4])
					i = i + 4
					ldPos := 0

					layerTarget := byte(1)
					if layerDecodeMode == 2 {
						layerTarget = byte(0)
					}

					for z := 0; z < 32; z++ {
						layerByte := byte(0)
						if (layerUsedBytes & uint32(0x80000000)) != 0 {
							layerByte = frameData[i]
							i = i + 1
							for layerSPos := 0; layerSPos < 8; layerSPos++ {
								layerData[layerId][layerLine][ldPos+layerSPos] = layerData[layerId][layerLine][ldPos+layerSPos] != (((layerByte >> uint32(layerSPos)) & 1) == layerTarget)
							}
						}

						ldPos = ldPos + 8
						layerUsedBytes <<= 1
					}

					if layerDecodeMode == 2 {
						for z := 0; z < 256; z++ {
							layerData[layerId][layerLine][z] = !layerData[layerId][layerLine][z]
						}
					}
					break
				case 3: // raw line
					for layerPos := 0; layerPos < 256; layerPos++ {
						layerData[layerId][layerLine][layerPos] = layerData[layerId][layerLine][layerPos] != ((frameData[i+(layerPos>>3)])&(1<<uint32(layerPos&7)) != 0)
					}
					i = i + 32
					break
				}
			}
		}
	}

	return nil
}

func ReadFlipnote(r io.Reader) (*Flipnote, error) {
	header := make([]byte, 0x6A0)
	r.Read(header)

	if !bytes.Equal(header[0:4], []byte("PARA")) {
		return nil, errors.New("Invalid magic!")
	}

	animationData := make([]byte, binary.LittleEndian.Uint32(header[4:8]))
	soundData := make([]byte, binary.LittleEndian.Uint32(header[8:12])+65536)
	r.Read(animationData)
	r.Read(soundData)

	flipnote := &Flipnote{}

	flipnote.Frames = make([]Frame, binary.LittleEndian.Uint16(header[12:14]))
	flipnote.Locked = binary.LittleEndian.Uint16(header[16:18]) != 0
	flipnote.CreatorName = utfSliceToString(header[20:42])
	flipnote.LastEditorName = utfSliceToString(header[42:64])
	flipnote.UserName = utfSliceToString(header[64:86])
	flipnote.CreatorId = binary.LittleEndian.Uint64(header[86:94])
	flipnote.LastEditorId = binary.LittleEndian.Uint64(header[94:102])
	flipnote.Filename = string(header[102:120])
	flipnote.OriginalFilename = string(header[120:138])
	flipnote.PreviousEditorId = binary.LittleEndian.Uint64(header[138:146])
	flipnote.Date = time.Date(2000, time.January, 1, 0, 0, int(binary.LittleEndian.Uint32(header[154:158])), 0, time.UTC)

	err := readAnimationData(flipnote, animationData)
	if err != nil {
		return nil, err
	}

	err = readSoundData(flipnote, soundData)
	if err != nil {
		return nil, err
	}

	return flipnote, nil
}
