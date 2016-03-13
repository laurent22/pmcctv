package main

// TODO: check shellPath logic - doesn't support DOS paths

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
	FrameDirPath      string `short:"d" long:"frame-dir" description:"Path to directory that will contain the captured frames. Default: ~/Pictures/pmcctv"`
	RemoteDir         string `short:"r" long:"remote-dir" description:"Remote location where frames will be saved to. Must contain a path compatible with scp (eg. user@someip:~/pmcctv)."`
	RemotePort        string `short:"p" long:"remote-port" description:"Port of remote location where frames will be saved to. If not set, whatever is the default scp port will be used (should be 22)."`
	BurstModeDuration int    `short:"b" long:"burst-mode-duration" description:"Duration of burst mode, in seconds. Set to -1 to disable burst mode altogether. Default: 10."`
	FramesTtl         int    `short:"t" long:"time-to-live" description:"For how long captured frames should be kept, in days. Default: 7."`
	InputDevice       string `short:"i" long:"input-device" description:"Name of capture input device. Default: auto-detect, except on Windows."`
}

var useRsync = false
var captureWorkerDone = make(chan bool)
var capturedFrames = make(chan string, 4096)

func captureFrame(filePath string, inputDevice string) error {
	// Linux: ffmpeg -y -loglevel fatal -f video4linux2 -i /dev/video0 -r 1 -t 0.0001 $FILENAME
	// OSX: $FFMPEG -loglevel fatal -f avfoundation -i "" -r 1 -t 0.0001 $FILENAME
	// Windows: ffmpeg -y -loglevel fatal -f dshow -i video="USB2.0 HD UVC WebCam" -r 1 -t 0.0001 test.jpg

	var args []string

	if runtime.GOOS == "linux" { // Linux
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "video4linux2",
			"-i", inputDevice,
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else if runtime.GOOS == "darwin" { // OSX
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "avfoundation",
			"-i", inputDevice,
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else if runtime.GOOS == "windows" { // Windows
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "dshow",
			"-i", "video=" + inputDevice,
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
	// On Windows, `compare` appears to always return an error code, even when successful
	// so the `parseInt` code after that will take care of checking if it's really an 
	// error or if it worked.
	if err != nil && runtime.GOOS != "windows" {
		return 0, errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	r, err := strconv.ParseInt(strings.Trim(string(buff), "\n\r\t "), 10, 64)
	if err != nil {
		return 0, errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}

	return int(r), nil
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

func multipleRemoteCopy(paths []string, opts CommandLineOptions) error {
	args := []string{}

	// It only makes sense to use rsync if there's more than one file to transfer
	if useRsync && len(paths) > 1 {
		// rsync -a <paths> <remote_dir>

		args = append(args, "-a")

		for _, path := range paths {
			args = append(args, shellPath(path))
		}

		if opts.RemotePort != "" {
			args = append(args, "-e")
			args = append(args, "ssh -p " + opts.RemotePort)
		}

		args = append(args, opts.RemoteDir)

		cmd := exec.Command("rsync", args...)
		buff, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
		}
	} else {
		// 	scp <path> <remote_dir>

		for _, path := range paths {
			args = append(args, shellPath(path))
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
		err := captureFrame(framePath, opts.InputDevice)
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
		var paths []string
		itemCount := len(capturedFrames)
		if itemCount <= 0 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		for i := 0; i < itemCount; i++ {
			f := <-capturedFrames
			paths = append(paths, f)
		}

		err := multipleRemoteCopy(paths, opts)
		if err != nil {
			fmt.Printf("Error: could not remote copy \"%s\": %s", paths, err)
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

func commandIsAvailable(commandName string) bool {
	err := exec.Command("type", commandName).Run()
	if err == nil {
		return true
	} else {
		err = exec.Command("sh", "-c", "type "+commandName).Run()
		if err == nil {
			return true
		}
	}
	return false
}

var cygwinCheckDone_ = false
var isCygwin_ = false

func isCygwin() bool {
	if cygwinCheckDone_ {
		return isCygwin_;
	}
	cygwinCheckDone_ = true
	r, err := exec.Command("uname", "-o").CombinedOutput()
	if err != nil {
		isCygwin_ = false
	} else {
		isCygwin_ = strings.ToLower(strings.Trim(string(r), "\r\n\t ")) == "cygwin"
	}
	return isCygwin_
}

func shellPath(path string) string {
	if isCygwin() {
		r, err := exec.Command("cygpath", "-u", path).CombinedOutput()
		if err != nil {
			fmt.Println("Error: cannot convert Cygwin path: %s", path) 
		}
		return strings.Trim(string(r), "\r\n\t ")
	}
	
	return filepath.ToSlash(path)
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

	if opts.InputDevice == "" {
		if runtime.GOOS == "linux" {
			opts.InputDevice = "/dev/video0"
		} else if runtime.GOOS == "darwin" {
			opts.InputDevice = ""
		} else {
			args = []string{
				"-hide_banner",
				"-list_devices",
				"true",
				"-f", "dshow",
				"-i", "dummy",
			}
			cmd := exec.Command("ffmpeg", args...)
			buff, _ := cmd.CombinedOutput()
			fmt.Println("Please specify the input device that should be used to capture the video. It can be any of the devices listed below under \"DirectShow video devices\":")
			fmt.Println("")
			fmt.Println("Then run the command again with the --input-device option. eg. pmcctv --input-device \"My USB WebCam\"")
			fmt.Println("");
			fmt.Println(string(buff))
			os.Exit(1)
		}
	}

	if opts.FrameDirPath == "" {
		u, err := user.Current()
		if err != nil {
			fmt.Println("No frame dir specified and cannot detect default Pictures dir. Please specify it with the --frame-dir option")
			os.Exit(1)
		}
		opts.FrameDirPath = u.HomeDir + "/Pictures/pmcctv"
	}

	opts.FrameDirPath = strings.TrimRight(opts.FrameDirPath, "/")

	os.MkdirAll(opts.FrameDirPath, 0700)

	fmt.Printf("Input device: %s\n", opts.InputDevice)
	fmt.Printf("Local frame dir: %s\n", opts.FrameDirPath)
	if opts.RemoteDir != "" {
		p := "Default"
		if opts.RemotePort != "" { p = opts.RemotePort } 
		fmt.Printf("Remote frame dir: %s Port: %s\n", opts.RemoteDir, p)
	}

	go captureWorker(opts)
	go cleanUpLocalFilesWorker(opts)

	if opts.RemoteDir != "" {
		useRsync = commandIsAvailable("rsync")
		go remoteCopyWorker(opts)
		go cleanUpRemoteFilesWorker(opts)
	}

	<-captureWorkerDone
}
