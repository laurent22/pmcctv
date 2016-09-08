package main

// TODO: check shellPath logic - doesn't support DOS paths
// TODO: allow specifying the capture device
// TODO: before running pmcctv, check that the specified video capture device exists and is working
// TODO: auto detect device on Windows
// TODO: "init" to setup pmcctv with good settings
// TODO: force pub key auth over SSH (since it's hard to automate anything otherwise)? -o PreferredAuthentications=pubkey -o PasswordAuthentication=no -o PubkeyAuthentication=yes
// TODO: refactor burstModeEnabled, so that it's an event that any worker can respond to. Currently, only videoWorker respond to it, and, if not running, the channel update block the application.
// TODO: better logging - error / trace

// # Setup the server
//
// The server is optional but is convenient to view the captured videos and images.
//
// 1. Copy the file index.php to your server.
// 2. Open the file index.php in a URL. For example, https://yourserver.com/path/to/index.php
// 3. Since not configuration is currently defined, it will ask you to create one. To do so, follow the instructions on screen.
// 4. Create the config.php file as instructed and upload it in the same directory as your server.
//
// # Setup the command line client
//
// 1. Run `pmcctv init` to setup the various parameters, including selecting the correct webcam, specifying the remote location, etc.
// 2. Run `pmcctv start` to start capturing frames.
// 3. Run `pmcctv --help` for additional information

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"log"
	"net/smtp"
	"net/mail"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "image/jpeg"
	_ "image/png"

	"github.com/jessevdk/go-flags"
	"github.com/scorredoira/email"
)

const VERSION = "1.0.0"

type StartCommandOptions struct {
	FfmpegPath         string  `          long:"ffmpeg" description:"Path to ffmpeg." default:"ffmpeg"`
	FrameDirPath       string  `short:"d" long:"frame-dir" description:"Path to directory that will contain the captured frames. (default: <PictureDirectory>/pmcctv)"`
	RemoteDir          string  `short:"r" long:"remote-dir" description:"Remote location where frames will be saved to. Must contain a path compatible with scp (eg. user@someip:~/pmcctv)."`
	RemotePort         string  `short:"p" long:"remote-port" description:"Port of remote location where frames will be saved to. If not set, whatever is the default scp port will be used (should be 22)."`
	BurstModeDuration  int     `          long:"burst-mode-duration" description:"Duration of burst mode, in seconds. Set to 0 to disable burst mode altogether." default:"30"`
	BurstModeFormat    string  `          long:"burst-mode-format" description:"Format of burst mode captured files, either \"image\" or \"video\"." default:"video"`
	BurstModeThreshold float32 `          long:"burst-mode-threshold" description:"How different two successive frames must be (as a percentage) for Burst Mode to be enabled." default:"1.0"`
	FramesTtl          int     `          long:"time-to-live" description:"For how long captured frames should be kept, in days." default:"7"`
	InputDeviceFormat  string  `short:"f" long:"input-device-format" description:"Format of capture input device. (default: auto-detect)"`
	InputDevice        string  `short:"i" long:"input-device" description:"Name of capture input device. (default: auto-detect)"`

	EmailFrom          string  `          long:"email-from" description:"Address from whom the email should be sent. To avoid being detected as spam, it's better to put your own, valid, email address."`
	EmailTo            string  `          long:"email-to" description:"Address to whom the email should be sent."`
	EmailSmtpDomain    string  `          long:"email-smtp-domain" description:"SMTP domain that should be used to send the email (eg. 'smtp.gmail.com')."`
	EmailSmtpPort      int     `          long:"email-smtp-port" description:"SMTP port that should be used to send the email (eg. '587')."`
	EmailSmtpPassword  string  `          long:"email-smtp-password" description:"Password to connect to the SMTP server."`
	EmailLinkBaseUrl   string  `          long:"email-link-base-url" description:"Base URL where the PMCCTV server is located. (eg. 'https://example.com/pmcctv/index.php')."`
}

type AppCommandOptions struct {
	Version bool `long:"version" description:"Display version information"`
}

type CommandOptions struct {
	App   AppCommandOptions
	Start StartCommandOptions
}

var useRsync = false
var captureWorkerDone = make(chan bool)
var burstModeEnabled = make(chan bool)
var burstModeDisabled = make(chan bool)
var filesToUpload = make(chan string, 4096)
var lastEmailTime = time.Time{}
var captureStartTime = time.Time{}

func imageDimensions(imagePath string) (int, int, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return 0, 0, err
	}

	defer file.Close()

	image, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}

	return image.Width, image.Height, nil
}

func sendEmail(opts StartCommandOptions, percentDiff float32, previousFramePath string, framePath string) {
	now := time.Now()
	var err error

	if now.Sub(captureStartTime) > time.Duration(60)*time.Second && (lastEmailTime.IsZero() || now.Sub(lastEmailTime) > time.Duration(20)*time.Second) {
		log.Println("Sending email...")

		subject := fmt.Sprintf("Motion detected [Diff: %.2f%%]", percentDiff)
		body := opts.EmailLinkBaseUrl
    
		m := email.NewMessage(subject, body)
		m.From = mail.Address{Name: "", Address: opts.EmailFrom}
		m.To = []string{opts.EmailTo}

		if err = m.Attach(previousFramePath); err != nil {
			log.Println("Email attachment error for %s: %s", previousFramePath, err)
		}

		if err = m.Attach(framePath); err != nil {
			log.Println("Email attachment error for %s: %s", framePath, err)
		}

		auth := smtp.PlainAuth("", opts.EmailFrom, opts.EmailSmtpPassword, opts.EmailSmtpDomain)
		if err = email.Send(opts.EmailSmtpDomain+":"+strconv.Itoa(opts.EmailSmtpPort), auth, m); err != nil {
			log.Println(err)
		}
	}
}

func captureFrame(filePath string, opts StartCommandOptions) error {
	// Linux: ffmpeg -y -loglevel fatal -f video4linux2 -i /dev/video0 -r 1 -t 0.0001 $FILENAME
	// OSX: $FFMPEG -loglevel fatal -f avfoundation -i "" -r 1 -t 0.0001 $FILENAME
	// Windows: ffmpeg -y -loglevel fatal -f dshow -i video="USB2.0 HD UVC WebCam" -r 1 -t 0.0001 test.jpg

	var args []string

	if runtime.GOOS == "linux" { // Linux
		// TODO: Fix input device and format
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "video4linux2",
			"-i", opts.InputDevice,
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else if runtime.GOOS == "darwin" { // Mac OS
		// TODO: Fix input device and format
		args = []string{
			"-y",
			"-loglevel", "error",
			"-f", "avfoundation",
			"-i", opts.InputDevice,
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else if runtime.GOOS == "windows" { // Windows
		args = []string{
			"-y",
			"-loglevel", "error",

			"-f", opts.InputDeviceFormat,
			"-i", opts.InputDevice,

			// "-f", "dshow",
			// "-i", "video=" + inputDevice,
			// "-f", "vfwcap",
			// "-i", "0",
			"-r", "1",
			"-t", "0.0001",
			filePath,
		}
	} else {
		panic("Unsupported OS: " + runtime.GOOS)
	}

	cmd := exec.Command(opts.FfmpegPath, args...)
	buff, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("ffmpeg: %s. %s. Command was: \"%s\" %s", err, string(buff), opts.FfmpegPath, strings.Join(args, " ")))
	}

	return nil
}

func captureVideo(filePath string, opts StartCommandOptions) (*exec.Cmd, error) {
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
			"-f", opts.InputDeviceFormat,
			"-i", opts.InputDevice,
			// "-f", "vfwcap",
			// "-i", "0",
			"-r", "30",
			"-c:v", "libvpx", // For Webm - https://trac.ffmpeg.org/wiki/Encode/VP8
			"-segment_time", "5",
			"-f", "segment",
			filePath,
		}
	} else {
		panic("Unsupported OS: " + runtime.GOOS)
	}

	cmd := exec.Command(opts.FfmpegPath, args...)
	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func compareFrames(path1 string, path2 string, diffPath string) (int, error) {
	//compare -fuzz 20% -metric ae $PREVIOUS_FILENAME $FILENAME diff.png 2> $DIFF_RESULT_FILE

	args := []string{
		"-fuzz", "15%",
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

func multipleRemoteCopy(paths []string, opts StartCommandOptions) error {
	args := []string{}

	// It only makes sense to use rsync if there's more than one file to transfer
	// if useRsync && len(paths) > 1 {
	// rsync -a <paths> <remote_dir>

	args = append(args, "-a")
	args = append(args, "--chmod=D700,F666")

	for _, path := range paths {
		args = append(args, shellPath(path))
	}

	if opts.RemotePort != "" {
		args = append(args, "-e")
		args = append(args, "ssh -p "+opts.RemotePort)
	}

	args = append(args, opts.RemoteDir)

	cmd := exec.Command("rsync", args...)
	buff, err := cmd.CombinedOutput()

	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	}
	// } else {
	// 	// 	scp <path> <remote_dir>

	// 	for _, path := range paths {
	// 		args = append(args, shellPath(path))
	// 	}

	// 	if opts.RemotePort != "" {
	// 		args = append(args, "-P")
	// 		args = append(args, opts.RemotePort)
	// 	}

	// 	args = append(args, opts.RemoteDir)

	// 	cmd := exec.Command("scp", args...)
	// 	buff, err := cmd.CombinedOutput()
	// 	if err != nil {
	// 		return errors.New(fmt.Sprintf("%s: %s", err, string(buff)))
	// 	}

	// }

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

			if opts.BurstModeFormat != "video" {
				break
			}

			log.Printf("[BM] Capturing video for %d seconds...\n", opts.BurstModeDuration)
			commandHasFinished = false
			videoFileBasePath = opts.FrameDirPath + "/cap_" + time.Now().Format("20060102T150405") + "_"
			// Need to record in flv format since it's more robust and
			// doesn't result in a corrupted file when killing the ffmpeg process.
			// Perhaps other formats have this benefit too (mp4 doesn't).
			videoPath := videoFileBasePath + "%03d.webm"
			cmd, err = captureVideo(videoPath, opts)
			if err != nil {
				log.Printf("Video capture error: %s\n", err)
			}

		case <-burstModeDisabled:

			if opts.BurstModeFormat != "video" {
				break
			}

			if cmd != nil {
				// Kill it twice since TERM sometime fails
				err = cmd.Process.Kill()
				if err != nil {
					log.Printf("Could not kill ffmpeg process (TERM signal - trying SIGKILL SIGNAL): %s\n", err)
				}
				time.Sleep(1 * time.Second) // Give it a chance to terminate properly
				err = cmd.Process.Signal(os.Kill)
				if err != nil {
					// Don't display anything for "Access is denied" error because it means the previous TERM signal
					// has already killed the process.
					if strings.Index(err.Error(), "Access is denied") < 0 {
						log.Printf("Could not kill ffmpeg process (SIGKILL signal): %s\n", err)
					}
				}
				cmd = nil
			}
			commandHasFinished = true
			log.Println("[BM] Done capturing video.")

		default:

			// Upload the videos if the video capture command is currently running
			// or if it has just finished running (to upload the last video that
			// was just recorded).

			if cmd != nil || commandHasFinished {
				commandHasFinished = false

				filePaths, err := filepath.Glob(videoFileBasePath + "*.webm")
				if err != nil {
					log.Printf("Cannot retrieve video file paths: %s\n", err)
					continue
				}

				for _, filePath := range filePaths {
					s, err := fileSize(filePath)
					if err != nil {
						log.Printf("Cannot retrieve video file size: %s\n", err)
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
			err := captureFrame(framePath, opts)
			if err != nil {
				log.Printf("Error: %s\n", err)
				continue
			}

			width, height, err := imageDimensions(framePath)
			if err != nil {
				log.Printf("Error: cannot get image dimensions: %s\n", err)
				continue
			}

			if previousFramePath != "" {
				diff, err := compareFrames(previousFramePath, framePath, framePath+".diff.png")
				if err != nil {
					log.Printf("Error: %s\n", err)
					diff = width * height
				}
				os.Remove(framePath + ".diff.png")
				burstModeMarker := ""
				if burstMode {
					burstModeMarker = "[BM] "
				}
				percentDiff := (float32(diff) / float32(width*height)) * 100
				if percentDiff < opts.BurstModeThreshold {
					log.Printf(burstModeMarker+"Same as previous image: delete (Diff = %.2f%%)\n", percentDiff)
					os.Remove(previousFramePath)
					previousFramePath = framePath
				} else {
					log.Printf(burstModeMarker+"Different image: keep (Diff = %.2f%%)\n", percentDiff)
					sendEmail(opts, percentDiff, previousFramePath, framePath)
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
		//     - Start capturing video
		//     - Don't capture still images, and don't run compareFrames()
		//     - After BurstModeDuration has elapsed, kill command, capture another frame and check if same as last capture frame.
		//         - If different => continue BurstMode with video capture
		//         - Otherwise => back to regular loop

		if opts.BurstModeDuration > 0 {
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
			log.Printf("Error: could not remote copy \"%s\": %s", paths, err)
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
	args = append(args, "cap_*.ogg")
	args = append(args, "-o")
	args = append(args, "cap_*.flv")
	args = append(args, "-o")
	args = append(args, "cap_*.mp4")
	args = append(args, "-o")
	args = append(args, "cap_*.webm")
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
		return isCygwin_
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
			log.Println("Error: cannot convert Cygwin path: %s", path)
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
	log.Printf("\n")
	log.Printf("For help with a particular command, type \"%s <command> --help\"\n", path.Base(os.Args[0]))
}

func initCommand() {
	reader := bufio.NewReader(os.Stdin)
	log.Print("Enter video device:")
	text, _ := reader.ReadString('\n')
	log.Println(text)

	args := []string{
		"start",
		"--input-device", "My Webcam",
	}

	var opts CommandOptions
	flagParser := createFlagParser(&opts)

	flagParser.ParseArgs(args)

	iniParser := flags.NewIniParser(flagParser)
	var iniOptions flags.IniOptions
	iniParser.WriteFile("/home/laurent/src/pmcctv_server/client/test.ini", iniOptions)
	os.Exit(0)
}

func createFlagParser(opts *CommandOptions) *flags.Parser {
	flagParser := flags.NewParser(&opts.App, flags.HelpFlag|flags.PassDoubleDash)

	flagParser.AddCommand(
		"start",
		"Start monitoring",
		"This is the main command, which is used to initialize pmcctv and start capturing video and monitoring.",
		&opts.Start,
	)

	return flagParser
}

// func ffmpegDevices() {
// 	args = []string{
// 		"-hide_banner",
// 		"-list_devices",
// 		"true",
// 		"-f", "dshow",
// 		"-i", "dummy",
// 	}
// 	cmd := exec.Command("ffmpeg", args...)
// 	buff, err := cmd.CombinedOutput()


// }

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error

	var opts CommandOptions
	flagParser := createFlagParser(&opts)

	args, err := flagParser.Parse()

	//initCommand()

	// cmd := exec.Command("scp", "-P", "2222", "/home/laurent/src/pmcctv_server/client/test.ini", "laurent@localhost:~/test_dest/")
	// buff, err := cmd.CombinedOutput()
	// log.Println(err)
	// log.Println(string(buff))

	// os.Exit(0)

	if err != nil {
		t := err.(*flags.Error).Type
		if t == flags.ErrHelp {
			printHelp(flagParser)
			os.Exit(0)
		} else if t == flags.ErrCommandRequired {
			// Here handle default flags (which are not associated with any command)
			if opts.App.Version {
				log.Println(VERSION)
				os.Exit(0)
			}
			printHelp(flagParser)
			os.Exit(0)
		} else {
			log.Printf("Error: %s\n", err)
			log.Printf("Type '%s --help' for more information.\n", path.Base(os.Args[0]))
			os.Exit(1)
		}
	}

	_ = args

	if opts.Start.InputDevice == "" {
		if runtime.GOOS == "linux" {
			opts.Start.InputDevice = "/dev/video0"
		} else if runtime.GOOS == "darwin" {
			opts.Start.InputDevice = ""
		} else {
			// args = []string{
			// 	"-hide_banner",
			// 	"-list_devices",
			// 	"true",
			// 	"-f", "dshow",
			// 	"-i", "dummy",
			// }
			// cmd := exec.Command("ffmpeg", args...)
			// buff, _ := cmd.CombinedOutput()
			// log.Println("Please specify the input device that should be used to capture the video. It can be any of the devices listed below under \"DirectShow video devices\":")
			// log.Println("")
			// log.Println("Then run the command again with the --input-device option. eg. pmcctv --input-device \"My USB WebCam\"")
			// log.Println("");
			// log.Println(string(buff))
			// os.Exit(1)
		}
	}

	if opts.Start.FrameDirPath == "" {
		u, err := user.Current()
		if err != nil {
			log.Println("No frame dir specified and cannot detect default Pictures dir. Please specify it with the --frame-dir option")
			os.Exit(1)
		}
		opts.Start.FrameDirPath = u.HomeDir + "/Pictures/pmcctv"
	}

	opts.Start.FrameDirPath = strings.TrimRight(opts.Start.FrameDirPath, "/")

	err = checkDependencies(opts.Start)

	if err != nil {
		log.Println("Some dependencies are missing. Please install them before continuing:")
		log.Println("")
		log.Println(err.Error())
		os.Exit(1)
	}

	os.MkdirAll(opts.Start.FrameDirPath, 0700)

	captureStartTime = time.Now()

	log.Printf("Input device format: %s\n", opts.Start.InputDeviceFormat)
	log.Printf("Input device: %s\n", opts.Start.InputDevice)
	log.Printf("Local frame dir: %s\n", opts.Start.FrameDirPath)
	log.Printf("Burst mode threshold: %.2f%%\n", opts.Start.BurstModeThreshold)

	if opts.Start.RemoteDir != "" {
		p := "Default"
		if opts.Start.RemotePort != "" {
			p = opts.Start.RemotePort
		}
		log.Printf("Remote frame dir: %s Port: %s\n", opts.Start.RemoteDir, p)
	}

	go captureVideoWorker(opts.Start)
	go captureWorker(opts.Start)
	go cleanUpLocalFilesWorker(opts.Start)

	if opts.Start.RemoteDir != "" {
		useRsync = commandIsAvailable("rsync")
		go remoteCopyWorker(opts.Start)
		go cleanUpRemoteFilesWorker(opts.Start)
	}

	<-captureWorkerDone
}
