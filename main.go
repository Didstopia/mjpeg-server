package main

import (
	"context"
	"flag"
	"image"
	"image/color"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	// Provides MJPEG support
	"github.com/mattn/go-mjpeg"

	// Provides camera support
	"gocv.io/x/gocv"
)

// TODO: We could utilize video4linux2 (v4l) to do the capture, instead of OpenCV?
//         https://github.com/korandiz/v4l/blob/master/demos/streamcam/streamcam.go
//         https://pkg.go.dev/github.com/korandiz/v4l

// TODO: I suppose we could either migrate to v4l or use both and let the user decide?
//       Note that OpenCV has multiple heavy dependencies, but is more cross-platform,
//       while v4l has very little dependencies but only works on linux.

// TODO: UPDATE: OpenCV seems _very_ heavy, at least on an M1 MBP, but it's really
//               the only choice when developing/testing, outside of linux.

var (
	// TODO: The camera id seems to come from OpenCV, but not sure if this is enough for our use case?
	// OpenCV camera id/index
	cameraId = flag.String("camera", "0", "Camera ID")

	// HTTP server's listening address and port (":<PORT>" will listen on all available IP addresses)
	serverAddress = flag.String("address", ":8080", "Server address")

	// TODO: With v4l, we might be able to figure this one out with "v4l2-ctl --list-formats-ext",
	//       as this will output both the timing and the FPS, per format.

	// FIXME: This should not only be calculated automatically from the input device,
	//        but also be clamped to the maximum value supported by the device.

	// Capture rate (frames per second?)
	frameRate = flag.Int("fps", 25, "Frames per second (frame rate)")
)

func capture(ctx context.Context, wg *sync.WaitGroup, stream *mjpeg.Stream) {
	// Always mark the wait group as done when the function finishes
	defer wg.Done()

	// TODO: How do we set resolution, input format etc.?
	// Create a new camera device
	log.Println("Initializing camera ...")
	var webcam *gocv.VideoCapture
	var err error
	if id, err := strconv.ParseInt(*cameraId, 10, 64); err == nil {
		webcam, err = gocv.VideoCaptureDevice(int(id))
		if err != nil {
			log.Println("WARNING: Failed to initialize camera from device:", err)
			return
		}
	} else {
		webcam, err = gocv.VideoCaptureFile(*cameraId)
		if err != nil {
			log.Println("Failed to initialize camera from file:", err)
			return
		}
	}
	if err != nil {
		log.Println("Failed to initialize camera:", err)
		return
	}

	// Close the camera when the function finishes
	defer webcam.Close()

	// Create a new "material" that will be used to store the latest frame
	log.Println("Initializing capture surface ...")
	im := gocv.NewMat()

	// Setup the frames per second calculation
	frameTime := time.Now()
	frameCount := 0
	framesPerSecond := int(*frameRate)

	log.Println("Starting capture ...")
	// Process camera frames until the context is done
	for len(ctx.Done()) == 0 {
		// FIXME: This doesn't seem to work right, as it keeps saying 60 FPS,
		//        even though we've specified a lower FPS, such as 30.
		//
		//        This is probably due to the fact that the FPS is calculated
		//        based on the time it takes to process the frame, not the time
		//        it takes to capture the frame, if that makes sense?

		// TODO: What if we artificially limit/delay/sleep the loop based on
		//       the original/user-specified frame rate?
		//       Wouldn't this achieve the correct results, while lowering
		//       the overall CPU usage for both the server and the client?

		// Calculate the real frames per second
		currentFrameTime := time.Now()
		frameCount++
		if (currentFrameTime.Sub(frameTime) / time.Second) >= 1 { // Last frame was >= 1 second ago
			framesPerSecond = int(1000.0 / float64(frameCount))
			frameCount = 0
			frameTime = currentFrameTime
			// frameTime = frameTime.Add(time.Second)
		}

		// FIXME: Can we use something like this to force the
		//        FPS calculation to be faster, as well as not
		//        increment to massive numbers over a long period of time?
		// if frameCount > 1000**frameRate {
		// 	frameTime = time.Now()
		// 	frameCount = 0
		// }

		// Prepare a buffer for the next frame
		var buf []byte

		// Check if we have a new frame
		if stream.NWatch() > 0 {
			// Attempt to read the next frame
			if ok := webcam.Read(&im); !ok {
				log.Println("Failed to read the next frame")
				continue
			}

			// FIXME: If possible, get the _real_ frame rate from the camera,
			//        instead of using the static, user configurable one.

			// Draw the current frame rate on the frame
			fpsText := "FPS: " + strconv.Itoa(framesPerSecond) + " (TARGET: " + strconv.Itoa(*frameRate) + ")"
			fpsTextScale := 4
			// gocv.PutText(&im, fpsText, image.Pt(int(10*float64(fpsTextScale)), int(20*float64(fpsTextScale))), gocv.FontHersheyPlain, float64(fpsTextScale), color.RGBA{0, 255, 0, 0}, 2)
			gocv.PutTextWithParams(
				&im,
				fpsText,
				image.Pt(
					int(10*float64(fpsTextScale)),
					int(20*float64(fpsTextScale)),
				),
				gocv.FontHersheyPlain,
				float64(fpsTextScale),
				color.RGBA{0, 255, 0, 0},
				1*fpsTextScale,
				gocv.Filled,
				false,
			)

			// rects := classifier.DetectMultiScale(im)
			// for _, r := range rects {
			// 	face := im.Region(r)
			// 	face.Close()
			// 	gocv.Rectangle(&im, r, color.RGBA{0, 0, 255, 0}, 2)
			// }

			// Encode the "material" image as a JPEG and store it in a buffer
			nbuf, err := gocv.IMEncode(".jpg", im)
			if err != nil {
				continue
			}

			// Store the frame in the buffer
			buf = nbuf.GetBytes()
		}

		// Update the MJPEG stream
		err = stream.Update(buf)
		if err != nil {
			if err.Error() == "stream was closed" {
				log.Println("Stream closed, aborting capture")
				break
			}
			log.Println("Failed to update MJPEG stream:", err)
			break
		}
	}

	log.Println("Capture finished")
}

func main() {
	// Parse the command line arguments
	log.Println("Parsing command line arguments ...")
	flag.Parse()

	// Calculate the stream interval from the frame rate
	log.Println("Calculating stream interval from frame rate:", *frameRate)
	streamInterval := time.Duration(1000/(*frameRate)) * time.Millisecond
	log.Println("Calculated stream interval:", streamInterval)

	// Create a new MJPEG stream
	log.Println("Initializing MJPEG stream ...")
	mjpegStream := mjpeg.NewStreamWithInterval(streamInterval)

	// Create a new cancelable context
	log.Println("Creating context ...")
	ctx, cancel := context.WithCancel(context.Background())

	// Create and configure a new wait group
	log.Println("Creating wait group ...")
	var wg sync.WaitGroup
	wg.Add(1)

	// Start the capture goroutine using the current context, wait group and MJPEG stream
	log.Println("Starting capture goroutine ...")
	go capture(ctx, &wg, mjpegStream)

	// Setup the MJPEG stream endpoint
	log.Println("Setting up MJPEG stream endpoint ...")
	http.HandleFunc("/video.mjpeg", mjpegStream.ServeHTTP)

	// Setup a static snapshot endpoint
	log.Println("Setting up snapshot endpoint ...")
	http.HandleFunc("/snapshot.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(mjpegStream.Current())
	})

	// TODO: Keep track of both inbound and outbound data and show stats on the web page (or on a separate page)

	// Setup an index page that shows the MJPEG stream
	log.Println("Setting up index page ...")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")

		w.Write([]byte(`<br>`))

		// TODO: While this works, it updates very slowly and seems pretty heavy?
		// w.Write([]byte(`<p>Stream Image</p>`))
		// w.Write([]byte(`<img src="/video.mjpeg" alt="MJPEG Stream Image" width="640" />`))

		// TODO: Inject custom CSS to adjust the stream and snapshot sizes etc.

		// FIXME: Why doesn't this work, yet the image based solutions work fine?
		w.Write([]byte(`<p>Stream Video</p>`))
		w.Write([]byte(`<video src="http://localhost:8080/video.mjpeg" alt="MJPEG Stream Video" controls autoplay width="640">`))
		w.Write([]byte(`  Your browser does not support the <code>video</code> element.`))
		w.Write([]byte(`</video>`))

		w.Write([]byte(`<br>`))

		// TODO: This works fine, it's just very, very large
		// w.Write([]byte(`<p>Stream Snapshot</p>`))
		// w.Write([]byte(`<img src="/snapshot.jpg" alt="MJPEG Stream Snapshot Image" width="640" />`))
	})

	// Create a new HTTP server
	log.Println("Creating HTTP server ...")
	server := &http.Server{Addr: *serverAddress}

	// Setup graceful shutdown
	log.Println("Setting up graceful shutdown ...")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)
	go func() {
		<-sc
		log.Println("Shutting down web server ...")
		server.Shutdown(ctx)
	}()

	// Start the web server
	log.Println("Starting web server on", *serverAddress)
	server.ListenAndServe()

	// Shutdown the MJPEG stream
	log.Println("Shutting down MJPEG stream ...")
	mjpegStream.Close()

	// Mark the context as canceled
	log.Println("Shutting down ...")
	cancel()

	// Wait until the wait group is done (capture goroutine has finished)
	wg.Wait()

	log.Println("Shutdown complete, terminating ...")
}
