package downloader

import (
	"bytes"
	"fmt"
	"github.com/metafates/mangal/color"
	"github.com/metafates/mangal/constant"
	"github.com/metafates/mangal/converter"
	"github.com/metafates/mangal/history"
	"github.com/metafates/mangal/key"
	"github.com/metafates/mangal/log"
	"github.com/metafates/mangal/open"
	"github.com/metafates/mangal/source"
	"github.com/metafates/mangal/style"
	"github.com/spf13/viper"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

// UpscaleImage uses waifu2x-ncnn-vulkan to upscale an image
func UpscaleImage(inputPath, outputPath string) error {
	cmd := exec.Command("waifu2x-ncnn-vulkan", "-i", inputPath, "-o", outputPath, "-n", "3", "-s", "4")
	// Add other options as needed

	// Create buffers to capture standard output and standard error
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Run the command
	err := cmd.Run()

	// Output the results
	fmt.Printf("stdout: %s\n", stdoutBuf.String())
	fmt.Printf("stderr: %s\n", stderrBuf.String())

	return err
}

// bufferToFile writes the contents of a bytes.Buffer to a temporary file and returns the file path
func bufferToFile(buf *bytes.Buffer, extension string) (string, error) {
	// Create a temporary file
	tmpFile, err := ioutil.TempFile("", "page-*."+extension)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Write buffer to file
	if _, err := buf.WriteTo(tmpFile); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

// fileToBuffer reads the contents of a file into a bytes.Buffer
func fileToBuffer(filePath string) (*bytes.Buffer, error) {
	// Read file contents
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Create a buffer and write data to it
	buf := bytes.NewBuffer(data)
	return buf, nil
}

// Read the chapter by downloading it with the given source
// and opening it with the configured reader.

func Read(chapter *source.Chapter, progress func(string)) error {
	if viper.GetBool(key.ReaderReadInBrowser) {
		return open.StartWith(
			chapter.URL,
			viper.GetString(key.ReaderBrowser),
		)
	}

	if viper.GetBool(key.DownloaderReadDownloaded) && chapter.IsDownloaded() {
		path, err := chapter.Path(false)
		if err == nil {
			return openRead(path, chapter, progress)
		}
	}

	log.Infof("downloading %s for reading. Provider is %s", chapter.Name, chapter.Source().ID())
	log.Infof("getting pages of %s", chapter.Name)
	progress("Getting pages")
	pages, err := chapter.Source().PagesOf(chapter)
	if err != nil {
		log.Error(err)
		return err
	}

	err = chapter.DownloadPages(true, progress)
	if err != nil {
		log.Error(err)
		return err
	}

	for _, page := range chapter.Pages {
		// Write the page's contents to a temporary file
		inputPath, err := bufferToFile(page.Contents, page.Extension)
		if err != nil {
			return err // Return early if there's an error
		}
		defer os.Remove(inputPath) // Schedule cleanup of the temp file

		// Define the output path for the upscaled image
		outputPath := strings.TrimSuffix(inputPath, ".jpeg") + "_upscaled..jpeg"

		// Upscale the image
		if err := UpscaleImage(inputPath, outputPath); err != nil {
			return err // Return early if there's an error
		}

		// Read the upscaled image back into the page's Contents
		upscaledBuffer, err := fileToBuffer(outputPath)
		if err != nil {
			return err // Return early if there's an error
		}
		defer os.Remove(outputPath) // Schedule cleanup of the upscaled file

		// Update the page's Contents with the upscaled image
		page.Contents = upscaledBuffer
	}

	log.Info("getting " + viper.GetString(key.FormatsUse) + " converter")
	conv, err := converter.Get(viper.GetString(key.FormatsUse))
	if err != nil {
		log.Error(err)
		return err
	}

	log.Info("converting " + viper.GetString(key.FormatsUse))
	progress(fmt.Sprintf(
		"Converting %d pages to %s %s",
		len(pages),
		style.Fg(color.Yellow)(viper.GetString(key.FormatsUse)),
		style.Faint(chapter.SizeHuman())),
	)
	path, err := conv.SaveTemp(chapter)
	if err != nil {
		log.Error(err)
		return err
	}

	err = openRead(path, chapter, progress)
	if err != nil {
		log.Error(err)
		return err
	}

	progress("Done")
	return nil
}

func openRead(path string, chapter *source.Chapter, progress func(string)) error {
	if viper.GetBool(key.HistorySaveOnRead) {
		go func() {
			err := history.Save(chapter)
			if err != nil {
				log.Warn(err)
			} else {
				log.Info("history saved")
			}
		}()
	}

	var (
		reader string
		err    error
	)

	switch viper.GetString(key.FormatsUse) {
	case constant.FormatPDF:
		reader = viper.GetString(key.ReaderPDF)
	case constant.FormatCBZ:
		reader = viper.GetString(key.ReaderCBZ)
	case constant.FormatZIP:
		reader = viper.GetString(key.ReaderZIP)
	case constant.FormatPlain:
		reader = viper.GetString(key.RaderPlain)
	}

	if reader != "" {
		log.Info("opening with " + reader)
		progress(fmt.Sprintf("Opening %s", reader))
	} else {
		log.Info("no reader specified. opening with default")
		progress("Opening")
	}

	err = open.RunWith(path, reader)
	if err != nil {
		log.Error(err)
		return fmt.Errorf("could not open %s with %s: %s", path, reader, err.Error())
	}

	log.Info("opened without errors")

	return nil
}
