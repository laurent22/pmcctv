# pmcctv - poor man's CCTV system in Go

This Go application captures images from a webcam with support for motion detection support, burst mode, and upload to a remote directory.

A Go worker captures frames at regular intervals using `ffmpeg`. Then ImageMagick's `compare` tool is used to check if this frame is similar to the previous one. If the frames are different enough, they are kept, otherwise they are deleted. This provide very simple motion detection and avoids filling up the hard drive with duplicate frames.

Optionally, a Go worker can be setup to automatically upload the frames to a remote server. Frames are copied using either `scp` or `rsync`, depending on what's available. Finally, another worker runs at regular intervals to clean up both the local and remote directory (by default, frames are kept for up to 7 days).

Normally, the program captures one frame per second. However, when motion is detected, a "burst mode" is activated, in which case frames will be captured as fast as possible for the next 10 seconds, or for as long as motion is being detected.

## Installation

### OS X

Install Go from https://golang.org/doc/install

    brew install ffmpeg
    brew install imagemagick
    go get -u github.com/laurent22/pmcctv
    go install github.com/laurent22/pmcctv

### Linux

Install Go from https://golang.org/doc/install

    sudo apt-get install ffmpeg
    sudo apt-get install imagemagick
    go get -u github.com/laurent22/pmcctv
    go install github.com/laurent22/pmcctv

Note: in many systems `avconv` (libav) is installed instead of ffmpeg. However since avconv video capture does not work well, only ffmpeg is supported.
 
### Windows

* Install Go from https://golang.org/doc/install
* Install [ffmpeg](http://ffmpeg.zeranoe.com/builds/)
* Install [ImageMagick](http://www.imagemagick.org/script/binary-releases.php)

From a command line prompt, run `go get -u github.com/laurent22/pmcctv && go install github.com/laurent22/pmcctv`

## Usage

    Usage:
      pmcctv [OPTIONS]

    Application Options:
      -m, --ffmpeg=              Path to ffmpeg.
      -d, --frame-dir=           Path to directory that will contain the capture frames. Default: ~/Pictures/pmcctv
      -r, --remote-dir=          Remote location where frames will be saved to. Must contain a path compatible with scp (eg. user@someip:~/pmcctv).
      -p, --remote-port=         Port of remote location where frames will be saved to. If not set, whatever is the default scp port will be used (should be 22).
      -b, --burst-mode-duration= Duration of burst mode, in seconds. Set to -1 to disable burst mode altogether. Default: 10
      -t, --time-to-live=        For how long captured frames should be kept, in days. Default: 7

    Help Options:
      -h, --help                 Show this help message
      
To stop the script, press Ctrl + C.

## TODO

* Allow specifying the video capture source and format for ffmpeg (curently hardcoded)
* Command line argument to specify the threshold for a frame to be kept.

## License

MIT
