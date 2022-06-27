package main

import (
	"context"
	"didstopia/mjpeg-server/udpserver"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/mattn/go-mjpeg"
)

const (
	defaultWebServerAddress = ":8080"
	defaultUdpServerAddress = ":8081"
	defaultFrameRate        = 25
)

var (
	webServerAddress = flag.String("web-address", defaultWebServerAddress, "Web server address/port")
	udpServerAddress = flag.String("udp-address", defaultUdpServerAddress, "UDP server address/port")
	frameRate        = flag.Int("fps", defaultFrameRate, "Frames per second (frame rate)")
)

func capture(ctx context.Context, wg *sync.WaitGroup, stream *mjpeg.Stream) {
	// Always mark the wait group as done when the function finishes
	defer wg.Done()

	// Create and start the UDP server
	udpServer := udpserver.NewUDPServer()
	go udpServer.Start()
	defer udpServer.Stop()

	// Keep track of frame time
	// now := time.Now()
	var now time.Time
	lastFrame := time.Now()

	// Process incoming frames until the context is done
	for len(ctx.Done()) == 0 {
		// Artificially limit the processing speed based on
		// how quickly we can process the incoming frames,
		// as well as what the current/desired frame rate is
		now = time.Now()
		delta := now.Sub(lastFrame)
		lastFrame = now
		if delta.Seconds() < float64(1/float64(*frameRate)) {
			time.Sleep(time.Duration(float64(1/float64(*frameRate))*1000) * time.Millisecond)
		}

		// Update the MJPEG stream
		err := stream.Update(udpServer.GetFrame())
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

	// Check for environment variable overrides
	if os.Getenv("MJPEG_SERVER_ADDRESS_WEB") != "" {
		*webServerAddress = os.Getenv("MJPEG_SERVER_ADDRESS_WEB")
		log.Println("Overriding web server address with", *webServerAddress)
	}
	if os.Getenv("MJPEG_SERVER_ADDRESS_UDP") != "" {
		*udpServerAddress = os.Getenv("MJPEG_SERVER_ADDRESS_UDP")
		log.Println("Overriding UDP server address with", *udpServerAddress)
	}
	if os.Getenv("MJPEG_SERVER_FRAMERATE") != "" {
		newFrameRate, err := strconv.Atoi(os.Getenv("MJPEG_SERVER_FRAMERATE"))
		if err != nil {
			log.Println("Failed to parse MJPEG_SERVER_FRAMERATE:", err, "(defaulting to", defaultFrameRate, "fps)")
			newFrameRate = defaultFrameRate
		}
		*frameRate = newFrameRate
		log.Println("Overriding frame rate with", *frameRate)
	}

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

	// TODO: Keep track of both inbound and outbound data and show stats on the web page (or on a separate page)

	// Setup an index page that shows the MJPEG stream
	log.Println("Setting up index page ...")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Handle action query parameter
		action := r.URL.Query().Get("action")
		if len(action) > 0 {
			if action == "stream" {
				// Return the MJPEG stream
				mjpegStream.ServeHTTP(w, r)
				return
			} else if action == "snapshot" {
				// Return the current frame as a JPEG
				w.Header().Set("Content-Type", "image/jpeg")
				w.Write(mjpegStream.Current())
				return
			} else {
				// Redirect back to index page
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}

		// Render the index page
		w.Header().Set("Content-Type", "text/html")

		w.Write([]byte(`<br>`))

		// TODO: While this works, it updates very slowly and seems pretty heavy?
		// w.Write([]byte(`<p>Stream Image</p>`))
		// w.Write([]byte(`<img src="/video.mjpeg" alt="MJPEG Stream Image" width="640" />`))

		// TODO: Inject custom CSS to adjust the stream and snapshot sizes etc.

		// FIXME: Why doesn't this work, yet the image based solutions work fine?
		// UPDATE: HTML <video> does NOT support MJPEG streams, only <img> does!
		w.Write([]byte(`<p>Stream Video</p>`))
		w.Write([]byte(`<img src="/?action=stream" alt="MJPEG Stream Video" width="640" />`))
		// w.Write([]byte(`<video src="/?action=stream" alt="MJPEG Stream Video" controls autoplay width="640">`))
		// // w.Write([]byte(`<video src="http://localhost:8080/?action=stream" alt="MJPEG Stream Video" controls autoplay width="640">`))
		// w.Write([]byte(`  Your browser does not support the <code>video</code> element.`))
		// w.Write([]byte(`</video>`))

		w.Write([]byte(`<br>`))

		// TODO: This works fine, it's just very, very large
		w.Write([]byte(`<p>Stream Snapshot</p>`))
		w.Write([]byte(`<img src="/?action=snapshot" alt="MJPEG Stream Snapshot Image" width="640" />`))
	})

	// Create a new HTTP server
	log.Println("Creating HTTP server ...")
	server := &http.Server{Addr: *webServerAddress}

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
	log.Println("Starting web server on", *webServerAddress)
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
