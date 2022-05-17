//
// Credits, original source and inspiration:
// https://ops.tips/blog/udp-client-and-server-in-go/
//

package udpserver

import (
	"context"
	"log"
	"net"
	"time"
)

type UDPServer struct {
	Port        string
	ctx         context.Context
	frameBuffer []byte
	lastFrame   []byte
}

// maxBufferSize specifies the size of the buffers that
// are used to temporarily hold data from the UDP packets
// that we receive.
const maxBufferSize = 65537 // Max segment size (https://github.com/corkami/formats/blob/master/image/jpeg.md)

// Create a new UDPServer with a default port
func NewUDPServer() *UDPServer {
	return NewUDPServerWithPort("8081")
}

// Create a new UDPServer with the given port
func NewUDPServerWithPort(port string) *UDPServer {
	log.Println("Creating new UDP server on port", port, "...")
	return &UDPServer{Port: port, ctx: context.Background()}
}

// Start the server
func (s *UDPServer) Start() {
	log.Println("Starting UDP server ...")

	// Start listening for incoming UDP packets
	conn, err := net.ListenPacket("udp", ":"+s.Port)
	if err != nil {
		log.Fatal(err)
	}

	// Close the connection automatically when done
	defer conn.Close()

	// Create a new buffer of sufficient size
	buffer := make([]byte, maxBufferSize)

	// Keep processing incoming data until the context is done
	for {
		select {
		case <-s.ctx.Done():
			log.Println("UDP server shutting down ...")
			return
		default:
			// By reading from the connection into the buffer, we block until there's
			// new content in the socket that we're listening for new packets.
			//
			// Whenever new packets arrive, `buffer` gets filled and we can continue
			// the execution.
			//
			// note.: `buffer` is not being reset between runs.
			//	  It's expected that only `n` reads are read from it whenever
			//	  inspecting its contents.
			n, _, err := conn.ReadFrom(buffer)
			if err != nil {
				log.Println("Error reading from UDP connection:", err)
				continue
			}

			// log.Printf("packet-received: bytes=%d from=%s\n", n, addr.String())

			// Setting a deadline for the `write` operation allows us to not block
			// for longer than a specific timeout.
			//
			// In the case of a write operation, that'd mean waiting for the send
			// queue to be freed enough so that we are able to proceed.
			deadline := time.Now().Add(time.Second * 5)
			err = conn.SetWriteDeadline(deadline)
			if err != nil {
				log.Println("Error setting write deadline:", err)
				return
			}

			// TODO: We need to properly protect against invalid incoming data

			// FIXME: This seems to always trigger, but that probably makes sense due to our small and incomplete buffer?
			// Skip if not a valid JPEG header
			// if buffer[0] != 0xff || buffer[1] != 0xd8 {
			// 	log.Println("Invalid JPEG header, skipping ...")
			// 	continue
			// }

			// Create a new buffer to hold the current frame
			newFameBuffer := append(s.frameBuffer, buffer[:n]...)

			// Skip if the new buffer is empty
			if len(newFameBuffer) <= 0 {
				log.Println("Empty frame buffer, ignoring packet ...")
				continue
			}

			// Check if the frame is a valid JPEG and if it's a complete frame
			hasJpegHeader := newFameBuffer[0] == 0xFF && newFameBuffer[1] == 0xD8
			hasJpegFooter := newFameBuffer[len(newFameBuffer)-2] == 0xFF && newFameBuffer[len(newFameBuffer)-1] == 0xD9
			isCompleteFrame := hasJpegHeader && hasJpegFooter

			// FIXME: If we use ffmpeg without "-re", the framerate varies,
			//        which causes some packets to not have the JPEG header, but not sure why?!
			// Abort if we didn't receive a JPEG header
			if !hasJpegHeader {
				log.Println("No JPEG header, ignoring packet ...")
				continue
			}

			// Store the new frame buffer
			s.frameBuffer = newFameBuffer

			//
			// Check if the frame buffer contains a complete JPEG image
			//
			// NOTE: JPEG image files begin with FF D8 and end with FF D9.
			//
			// log.Println(fmt.Sprintf("%02x", s.frameBuffer[0]), fmt.Sprintf("%02x", s.frameBuffer[1]), fmt.Sprintf("%02x", s.frameBuffer[2]), fmt.Sprintf("%02x", s.frameBuffer[3]), "hasJpegHeader:", hasJpegHeader, "hasJpegFooter:", hasJpegFooter)
			if isCompleteFrame {
				// Copy the frame buffer to the last frame
				s.lastFrame = make([]byte, len(s.frameBuffer))
				copy(s.lastFrame, s.frameBuffer)

				// TODO: Logging here, as well as using the bytesize library,
				//       seems to significantly slow down our speed of processing the individual frames
				// log.Println("Frame received:" /*string(s.frameBuffer),*/, bytesize.New(float64(len(s.lastFrame))), "from:", addr.String())

				// Reset the frame buffer
				s.frameBuffer = []byte{}
			}

			// TODO: Do we need to return anything back to ffmpeg?

			// Write the packet's contents back to the client.
			// n, err = conn.WriteTo(buffer[:n], addr)
			// if err != nil {
			// 	log.Println("Error writing to UDP connection:", err)
			// 	return
			// }

			// log.Printf("packet-written: bytes=%d to=%s\n", n, addr.String())
		}
	}
}

// Stop the server
func (s *UDPServer) Stop() {
	log.Println("Stopping UDP server ...")
	s.ctx.Done()
}

// Get the current frame
func (s *UDPServer) GetFrame() []byte {
	return s.lastFrame
}
