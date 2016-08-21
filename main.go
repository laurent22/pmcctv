package main

// TODO: check shellPath logic - doesn't support DOS paths
// TODO: allow specifying the capture device
// TODO: specify tolerance as a percentage (currently set to number of different pixels)
// TODO: add video support for Linux
// TODO: add video support for Mac OS
// TODO: before running pmcctv, check that the specified video capture device exists and is working
// TODO: set burst mode to 0 to disable
// TODO: specify default in command
// TODO: auto detect device on Windows
// TODO: "init" to setup pmcctv with good settings

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
)

type StartCommandOptions struct {
	FfmpegPath        string `short:"m" long:"ffmpeg" description:"Path to ffmpeg." default:"ffmpeg"`
	FrameDirPath      string `short:"d" long:"frame-dir" description:"Path to directory that will contain the captured frames. (default: <PictureDirectory>/pmcctv)"`
	RemoteDir         string `short:"r" long:"remote-dir" description:"Remote location where frames will be saved to. Must contain a path compatible with scp (eg. user@someip:~/pmcctv)."`
	RemotePort        string `short:"p" long:"remote-port" description:"Port of remote location where frames will be saved to. If not set, whatever is the default scp port will be used (should be 22)."`
	BurstModeDuration int    `short:"b" long:"burst-mode-duration" description:"Duration of burst mode, in seconds. Set to -1 to disable burst mode altogether." default:"10"`
	BurstModeFormat   string `short:"f" long:"burst-mode-format" description:"Format of burst mode captured files, either \"image\" or \"video\"." default:"video"`
	FramesTtl         int    `short:"t" long:"time-to-live" description:"For how long captured frames should be kept, in days." default:"7"`
	InputDevice       string `short:"i" long:"input-device" description:"Name of capture input device. Default: auto-detect."`
}

type CommandLineOptions struct {
	Version bool `short:"v" long:"version" description:"Display version information"`
}

var useRsync = false
var captureWorkerDone = make(chan bool)
var burstModeEnabled = make(chan bool)
var burstModeDisabled = make(chan bool)
var filesToUpload = make(chan string, 4096)

func captureFrame(ffmpegPath string, filePath string, inputDevice string) error {
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
	} else if runtime.GOOS == "darwin" { // Mac OS
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
			// "-f", "dshow",
			"-f", "vfwcap",
			// "-i", "video=" + inputDevice,
			"-i", "0",
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else {
		panic("Unsupported OS: " + runtime.GOOS)
	}

	cmd := exec.Command(ffmpegPath, args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		println(string(buff))
		return errors.New(fmt.Sprintf("ffmpeg: %s. %s. Command was: \"%s\" %s", err, string(buff), ffmpegPath, strings.Join(args, " ")))
	}

	return nil
}

func captureVideo(ffmpegPath string, filePath string, inputDevice string) (*exec.Cmd, error) {
	// Linux: TODO
	// OSX: TODO
	// Windows: ffmpeg -y -f vfwcap -r 25 -i 0 -segment_time 1 -f segment "capture-%03d.flv"

	var args []string

	if runtime.GOOS == "linux" { // Linux
		panic("Not implemented: " + runtime.GOOS)
	} else if runtime.GOOS == "darwin" { // Mac OS
		panic("Not implemented: " + runtime.GOOS)
	} else if runtime.GOOS == "windows" { // Windows
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "vfwcap",
			"-r", "30",
			"-i", "0",
			"-segment_time", "5",
			"-f", "segment",
			filePath,
		}
	} else {
		panic("Unsupported OS: " + runtime.GOOS)
	}

	cmd := exec.Command(ffmpegPath, args...)
	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return cmd, nil
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

func remoteCopy(path string, opts StartCommandOptions) error {
	// scp <path> <remote_dir>

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

func multipleRemoteCopy(paths []string, opts StartCommandOptions) error {
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
		// scp <path> <remote_dir>

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

func fileSize(filePath string) (int64, error) {
	file, err := os.Open(filePath) 
	if err != nil {
		return 0, err
	}
	fi, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func captureVideoWorker(opts StartCommandOptions) {
	var cmd *exec.Cmd
	var err error
	var videoFileBasePath string
	uploadedVideoFiles := make(map[string]bool)
	commandHasFinished := false

	for {
		select {

		case <-burstModeEnabled:

			fmt.Printf("Burst mode: capturing video for %d seconds...\n", opts.BurstModeDuration)
			commandHasFinished = false
			videoFileBasePath = opts.FrameDirPath + "/cap_" + time.Now().Format("20060102T150405") + "_"
			// Need to record in flv format since it's more robust and
			// doesn't result in a corrupted file when killing the ffmpeg process.
			// Perhaps other formats have this benefit too (mp4 doesn't).
			videoPath := videoFileBasePath + "%03d.flv"
			cmd, err = captureVideo(opts.FfmpegPath, videoPath, "")
			if err != nil {
				fmt.Printf("Video capture error: %s\n", err)
			}

		case <-burstModeDisabled:
			
			if cmd != nil {
				cmd.Process.Kill()
				cmd = nil
			}
			commandHasFinished = true
			fmt.Println("Burst mode: done capturing video.")

		default:

			// Upload the videos if the video capture command is currently running
			// or if it has just finished running (to upload the last video that
			// was just recorded).

			if cmd != nil || commandHasFinished {
				commandHasFinished = false

				filePaths, err := filepath.Glob(videoFileBasePath + "*.flv")
				if err != nil {
					fmt.Printf("Cannot retrieve video file paths: %s\n", err)
					continue;
				}

				for _, filePath := range filePaths {
					s, err := fileSize(filePath)
					if err != nil {
						fmt.Printf("Cannot retrieve video file size: %s\n", err)
						continue
					}

					// The key is <filename>_<filesize>. This is because files are being uploaded
					// as they are being created by ffmpeg, so on the first upload we might upload only
					// a partial file. On the next loop we check again the size - if it has changed,
					// we upload again, etc. If it hasn't changed, it means ffmepg has started creating
					// the next video segment.
					k := fmt.Sprintf("%s_%d", filePath, s)
					if _, ok := uploadedVideoFiles[k]; ok {
						continue
					}
					uploadedVideoFiles[k] = true
					filesToUpload <- filePath
				}

				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func captureWorker(opts StartCommandOptions) {
	previousFramePath := ""
	lastMotionTime := time.Now().Add(-60 * time.Second)
	burstMode := false

	for {
		now := time.Now()


		if burstMode && opts.BurstModeFormat == "video" {

		} else {
			baseName := "cap_" + now.Format("20060102T150405") + "_" + fmt.Sprintf("%09d", now.Nanosecond())
			framePath := opts.FrameDirPath + "/" + baseName + ".jpg"
			err := captureFrame(opts.FfmpegPath, framePath, opts.InputDevice)
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
				// if diff <= 20 {
				if diff <= 5000 {
					fmt.Printf(burstModeMarker+"Same as previous image: delete (Diff = %d)\n", diff)
					os.Remove(framePath)
				} else {
					fmt.Printf(burstModeMarker+"Different image: keep (Diff = %d)\n", diff)
					filesToUpload <- framePath
					previousFramePath = framePath
					lastMotionTime = now
				}
			} else {
				filesToUpload <- framePath
				previousFramePath = framePath
			}
		}



		// If video capture is enabled:
		// 		 Start capturing video
		//       Don't capture still images, and don't run compareFrames()
		//       After BurstModeDuration has elapsed, kill command, capture another frame and check if same as last capture frame.
		//           If different => continue BurstMode with video capture
		//           Otherwise => back to regular loop

		if opts.BurstModeDuration >= 0 {
			previousBurstMode := burstMode
			burstMode = now.Sub(lastMotionTime) <= time.Duration(opts.BurstModeDuration)*time.Second
			if burstMode != previousBurstMode {
				if burstMode {
					burstModeEnabled <- true
				} else {
					burstModeDisabled <- true
				}
			}
		}

		waitingTime := 1000 * time.Millisecond
		if burstMode {
			waitingTime = 15 * time.Millisecond
		}
		time.Sleep(waitingTime)
	}
}

func remoteCopyWorker(opts StartCommandOptions) {
	for {
		var paths []string
		itemCount := len(filesToUpload)
		if itemCount <= 0 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		for i := 0; i < itemCount; i++ {
			f := <-filesToUpload
			paths = append(paths, f)
		}

		err := multipleRemoteCopy(paths, opts)
		if err != nil {
			fmt.Printf("Error: could not remote copy \"%s\": %s", paths, err)
		}
	}
}

func cleanUpLocalFilesWorker(opts StartCommandOptions) {
	for {
		cleanUpLocalFiles(opts)
		time.Sleep(1 * time.Hour)
	}
}

func cleanUpRemoteFilesWorker(opts StartCommandOptions) {
	for {
		cleanUpRemoteFiles(opts)
		time.Sleep(1 * time.Hour)
	}
}

func cleanUpLocalFiles(opts StartCommandOptions) error {
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
	args = append(args, "-o")
	args = append(args, "cap_*.flv")
	args = append(args, "-mtime")
	args = append(args, "+"+strconv.Itoa(framesTtl))
	args = append(args, "-delete")
	return args
}

func cleanUpRemoteFiles(opts StartCommandOptions) error {
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

func checkDependencies(opts StartCommandOptions) error {
	var s []string
	if !commandIsAvailable(opts.FfmpegPath) {
		s = append(s, "\"ffmpeg\" command not found. Please install it or specify the path via the --ffmpeg parameter.")
	}
	if !commandIsAvailable("compare") {
		s = append(s, "\"compare\" command not found. Please install the ImageMagick package on your system.")
	}
	if !commandIsAvailable("identify") {
		s = append(s, "\"identify\" command not found. Please install the ImageMagick package on your system.")
	}
	if len(s) > 0 {
		return errors.New(strings.Join(s, "\n"))
	} else {
		return nil
	}	
}

func printHelp(flagParser *flags.Parser) {
	flagParser.WriteHelp(os.Stdout)
	fmt.Printf("\n")
	fmt.Printf("For help with a particular command, type \"%s <command> --help\"\n", path.Base(os.Args[0]))	
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error

	var opts StartCommandOptions
	var commandLineOptions CommandLineOptions
	flagParser := flags.NewParser(&commandLineOptions, flags.HelpFlag|flags.PassDoubleDash)

	flagParser.AddCommand(
		"start",
		"Start monitoring",
		"This is the main command, which is used to initialize pmcctv and start capturing video and monitoring.",
		&opts,
	)

	args, err := flagParser.Parse()
	if err != nil {
		t := err.(*flags.Error).Type
		if t == flags.ErrHelp {
			printHelp(flagParser)
			os.Exit(0)
		} else if t == flags.ErrCommandRequired {
			// Here handle default flags (which are not associated with any command)
			if commandLineOptions.Version {
				// TODO: Print version and exit
				os.Exit(0)
			}
			printHelp(flagParser)
			os.Exit(0)
		} else {
			fmt.Printf("Error: %s\n", err)
			fmt.Printf("Type '%s --help' for more information.\n", path.Base(os.Args[0]))
			os.Exit(1)
		}
	}

	_ = args

	if opts.BurstModeFormat == "" {
		opts.BurstModeFormat = "video"
	}

	if opts.FfmpegPath == "" {
		opts.FfmpegPath = "ffmpeg"
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

	err = checkDependencies(opts)

	if err != nil {
		fmt.Println("Some dependencies are missing. Please install them before continuing:")
		fmt.Println("")
		fmt.Println(err.Error())
		os.Exit(1)
	}

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

	if opts.BurstModeFormat == "video" {
		go captureVideoWorker(opts)
	}

	if opts.RemoteDir != "" {
		useRsync = commandIsAvailable("rsync")
		go remoteCopyWorker(opts)
		go cleanUpRemoteFilesWorker(opts)
	}

	<-captureWorkerDone
}
