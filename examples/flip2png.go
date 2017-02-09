package main

import (
	"fmt"
	"github.com/asiekierka/go-flipnotetools"
	"github.com/cryptix/wav"
	"image/png"
	"os"
)

func check(e error) {
    if e != nil {
        panic(e)
    }
}

func main() {
	file, err := os.Open(os.Args[1])
	check(err)

	flipnote, err := flipnotetools.ReadFlipnote(file)
	check(err)

	file.Close()

	os.Stderr.WriteString(fmt.Sprint("Creator: ", flipnote.CreatorName, "\n"))
	os.Stderr.WriteString(fmt.Sprint("Last Editor: ", flipnote.LastEditorName, "\n"))
	os.Stderr.WriteString(fmt.Sprint("User: ", flipnote.UserName, "\n"))
	os.Stderr.WriteString(fmt.Sprint("Date: ", flipnote.Date, "\n"))
	os.Stderr.WriteString(fmt.Sprint("Filename: ", flipnote.Filename, "\n"))

	frameDuration := flipnote.FrameDuration()
	audioData, audioFreq := flipnote.MixedSoundAsPCM()
	fmt.Println(1.0 / frameDuration)

	err = os.Mkdir("./out/", 0777)

	wavFile, err := os.Create("./out/audio.wav")
	check(err)

	wavMeta := wav.File {
		Channels: 1,
		SampleRate: uint32(audioFreq),
		SignificantBits: 16,
	}

	wavWriter, err := wavMeta.NewWriter(wavFile)
	check(err)
	sample := make([]byte, 2)

	for i := range audioData {
		sample[0] = byte(audioData[i] & 0xFF)
		sample[1] = byte(audioData[i] >> 8)
		wavWriter.WriteSample(sample)
	}

	wavWriter.Close()
	wavFile.Close()

	for i := range flipnote.Frames {
		out, err := os.Create(fmt.Sprintf("./out/%05d.png", i))
		check(err)

		png.Encode(out, flipnote.Frames[i].Image())
		out.Close()
	}
}
