package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
)

type CommandLineOptions struct {
	FfmpegPath        string `short:"m" long:"ffmpeg" description:"Path to ffmpeg."`
	FrameDirPath      string `short:"d" long:"frame-dir" description:"Path to directory that will contain the capture frames. Default: ~/Pictures/pmcam"`
	RemoteDir         string `short:"r" long:"remote-dir" description:"Remote location where frames will be saved to. Must contain a path compatible with scp (eg. user@someip:~/pmcam)."`
	RemotePort        string `short:"p" long:"remote-port" description:"Remote location port where frames will be saved to. If not set, whatever is the default scp port will be used (should be 22)."`
	BurstModeDuration int    `short:"b" long:"burst-mode-duration" description:"Duration of burst mode, in seconds. Set to -1 to disable burst mode altogether. Default: 10"`
	FramesTtl         int    `short:"t" long:"time-to-live" description:"For how long captured frames should be kept, in days. Default: 7"`
}

var captureWorkerDone = make(chan bool)
var capturedFrames = make(chan string, 4096)

func captureFrame(filePath string) error {
	// Linux: ffmpeg -y -loglevel fatal -f video4linux2 -i /dev/video0 -r 1 -t 0.0001 $FILENAME
	// OSX: $FFMPEG -loglevel fatal -f avfoundation -i "" -r 1 -t 0.0001 $FILENAME

	var args []string

	if runtime.GOOS == "linux" { // Linux
		args = []string{
			"-y",
			"-loglevel", "fatal",
			"-f", "video4linux2",
			"-i", "/dev/video0",
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else if runtime.GOOS == "darwin" { // OSX
		args = []string{
			"-y",
			"-loglevel", "fatal",
			"-f", "avfoundation",
			"-i", "",
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else {
		panic("Unsupported OS: " + runtime.GOOS)
	}

	cmd := exec.Command("ffmpeg", args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	return nil
}

func compareFrames(path1 string, path2 string, diffPath string) (int, error) {
	//compare -fuzz 20% -metric ae $PREVIOUS_FILENAME $FILENAME diff.png 2> $DIFF_RESULT_FILE

	args := []string{
		"-fuzz", "20%",
		"-metric", "ae",
		path1,
		path2,
		diffPath,
	}

	cmd := exec.Command("compare", args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	r, err := strconv.ParseInt(strings.Trim(string(buff), "\n\r\t "), 10, 64)
	return int(r), err
}

func remoteCopy(path string, opts CommandLineOptions) error {
	// 	scp <path> <remote_dir>

	args := []string{
		path,
	}

	if opts.RemotePort != "" {
		args = append(args, "-P")
		args = append(args, opts.RemotePort)
	}

	args = append(args, opts.RemoteDir)

	cmd := exec.Command("scp", args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}
	return nil
}

func captureWorker(opts CommandLineOptions) {
	previousFramePath := ""
	lastMotionTime := time.Now().Add(-60 * time.Second)
	burstMode := false

	for {
		now := time.Now()
		baseName := "cap_" + now.Format("20060102T150405") + "_" + fmt.Sprintf("%09d", now.Nanosecond())
		framePath := opts.FrameDirPath + "/" + baseName + ".jpg"
		err := captureFrame(framePath)
		if err != nil {
			fmt.Printf("Error: %s\n", err)
			continue
		}

		if previousFramePath != "" {
			diff, err := compareFrames(previousFramePath, framePath, framePath+".diff.png")
			if err != nil {
				fmt.Printf("Error: %s\n", err)
				diff = 9999999
			}
			os.Remove(framePath + ".diff.png")
			burstModeMarker := ""
			if burstMode {
				burstModeMarker = "[BM] "
			}
			if diff <= 20 {
				fmt.Printf(burstModeMarker+"Same as previous image: delete (Diff = %d)\n", diff)
				os.Remove(framePath)
			} else {
				fmt.Printf(burstModeMarker+"Different image: keep (Diff = %d)\n", diff)
				capturedFrames <- framePath
				previousFramePath = framePath
				lastMotionTime = now
			}
		} else {
			capturedFrames <- framePath
			previousFramePath = framePath
		}

		if opts.BurstModeDuration >= 0 {
			burstMode = now.Sub(lastMotionTime) <= time.Duration(opts.BurstModeDuration)*time.Second
		}

		waitingTime := 1000 * time.Millisecond
		if burstMode {
			waitingTime = 15 * time.Millisecond
		}
		time.Sleep(waitingTime)
	}
}

func remoteCopyWorker(opts CommandLineOptions) {
	for {
		capturedFrame := <-capturedFrames
		err := remoteCopy(capturedFrame, opts)
		if err != nil {
			fmt.Printf("Error: could not remote copy \"%s\": %s", capturedFrame, err)
		}
	}
}

func cleanUpLocalFilesWorker(opts CommandLineOptions) {
	for {
		cleanUpLocalFiles(opts)
		time.Sleep(1 * time.Hour)
	}
}

func cleanUpRemoteFilesWorker(opts CommandLineOptions) {
	for {
		cleanUpRemoteFiles(opts)
		time.Sleep(1 * time.Hour)
	}
}

func cleanUpLocalFiles(opts CommandLineOptions) error {
	args := []string{}
	args = appendCleanUpFindCommandArgs(args, opts.FrameDirPath, opts.FramesTtl)
	cmd := exec.Command("find", args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	return nil
}

func appendCleanUpFindCommandArgs(args []string, dir string, framesTtl int) []string {
	args = append(args, dir)
	args = append(args, "-name")
	args = append(args, "cap_*.jpg")
	args = append(args, "-mtime")
	args = append(args, "+"+strconv.Itoa(framesTtl))
	args = append(args, "-delete")
	return args
}

func cleanUpRemoteFiles(opts CommandLineOptions) error {
	args := []string{}

	s := strings.Split(opts.RemoteDir, ":")
	userHost := s[0]
	dir := "."
	if len(s) >= 2 {
		dir = s[1]
	}

	if opts.RemotePort != "" {
		args = append(args, "-p")
		args = append(args, opts.RemotePort)
	}

	args = append(args, userHost)
	args = append(args, "find")
	args = appendCleanUpFindCommandArgs(args, dir, opts.FramesTtl)

	cmd := exec.Command("ssh", args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	return nil
}

func fileTime(filePath string) (time.Time, error) {
	// /path/to/cap_20151023T113103_058764637.jpg
	basename := filepath.Base(filePath)
	s := strings.Split(basename, "_")
	if len(s) <= 1 {
		return time.Time{}, errors.New("Invalid filename: " + filePath)
	}
	return time.Parse("20060102T150405", s[1])
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error

	var opts CommandLineOptions
	flagParser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	args, err := flagParser.Parse()
	if err != nil {
		t := err.(*flags.Error).Type
		if t == flags.ErrHelp {
			flagParser.WriteHelp(os.Stdout)
			os.Exit(0)
		} else {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
	}

	_ = args

	if opts.BurstModeDuration == 0 {
		opts.BurstModeDuration = 10
	}

	if opts.FramesTtl == 0 {
		opts.FramesTtl = 7
	}

	if opts.FrameDirPath == "" {
		u, err := user.Current()
		if err != nil {
			fmt.Println("No frame dir specified and cannot detect default Pictures dir. Please specify it with the --frame-dir option")
			os.Exit(1)
		}
		opts.FrameDirPath = u.HomeDir + "/Pictures/pmcam"
	}

	opts.FrameDirPath = strings.TrimRight(opts.FrameDirPath, "/")

	os.MkdirAll(opts.FrameDirPath, 0700)

	go captureWorker(opts)
	go cleanUpLocalFilesWorker(opts)

	if opts.RemoteDir != "" {
		go remoteCopyWorker(opts)
		go cleanUpRemoteFilesWorker(opts)
	}

	<-captureWorkerDone
}
