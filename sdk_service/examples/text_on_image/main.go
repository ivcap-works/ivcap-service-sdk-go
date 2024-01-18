package main

import (
	// "context"

	"flag"
	"image"
	"image/color"
	"io"

	// Package image/jpeg is not used explicitly in the code below,
	// but is imported for its initialization side-effect, which allows
	// image.Decode to understand JPEG formatted images.
	// _ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	log "go.uber.org/zap"

	"github.com/fogleman/gg"
	ivcap "github.com/ivcap-works/ivcap-sdk-go"
)

var (
	Version   string
	GitCommit = "unknown"
	GitTag    = "unknown"
	BuildDate = "unknown"

	logger, _ = log.NewDevelopment()
	ivcapEnv  *ivcap.Environment
)

const (
	W = 1024
	H = 512

	MAX_ATTEMPTS = 10

	// referencing the otehr packages blows up Docker - keep an eye on this
	// ARTIFACT_ID_HEADER            = "X-Artifact-Id"            // tus.ARTIFACT_HEADER
	// NAME_HEADER                   = "X-Name"                   // tus.NAME_HEADER
	// META_DATA_FOR_ARTIFACT_HEADER = "X-Meta-Data-For-Artifact" // metadata.META_DATA_FOR_ARTIFACT_HEADER
	// META_DATA_SCHEMA_HEADER       = "X-Meta-Data-Schema"       // metadata.META_DATA_SCHEMA_HEADER

	// ORDER_ID_ENV    = "IVCAP_ORDER_ID"
	// STORAGE_URL_ENV = "IVCAP_STORAGE_URL"
	// STORAGE_URL_DEF = "http://localhost:8888"
	// CACHE_URL_ENV   = "IVCAP_CACHE_URL"
	// READYZ          = "/readyz"
)

func draw(msg string, img image.Image, oid string, w int, h int, writer io.Writer) {
	wf := float64(w)
	hf := float64(h)
	dc := gg.NewContext(w, h)

	dc.SetRGB(0, 0, 0)
	if err := dc.LoadFontFace("./CaveatBrush-Regular.ttf", 32); err != nil {
		logger.Error("Loading font", log.Int("size", 32), log.Error(err))
	}
	dc.DrawStringAnchored("Order: "+oid, wf-30, hf-20, 1.0, 0)

	// draw text
	dc.SetRGB(0, 0, 0)
	if err := dc.LoadFontFace("./CaveatBrush-Regular.ttf", 128); err != nil {
		logger.Error("Loading font", log.Int("size", 128), log.Error(err))
	}
	dc.DrawStringAnchored(msg, wf/2, hf/2, 0.5, 0.5)

	// get the context as an alpha mask
	mask := dc.AsMask()

	// clear the context
	dc.SetRGB(1, 1, 1)
	dc.Clear()

	if img != nil {
		dc.DrawImage(img, 0, 0)
	}
	// set a gradient
	g := gg.NewLinearGradient(0, 0, wf, hf)
	g.AddColorStop(0, color.RGBA{255, 0, 0, 255})
	g.AddColorStop(1, color.RGBA{0, 0, 255, 255})
	dc.SetFillStyle(g)

	// using the mask, fill the context with the gradient
	dc.SetMask(mask)
	dc.DrawRectangle(0, 0, W, H)
	dc.Fill()

	dc.EncodePNG(writer)
}

func getImage(url string) (img image.Image, err error) {
	logger.Info("downloading image", log.String("url", url))
	err = ivcapEnv.GetResource(url, func(reader io.Reader) error {
		img, _, err = image.Decode(reader)
		return err
	})
	return
}

type Meta struct {
	Msg                string `json:"message,omitempty"`
	BackgroundUrl      string `json:"background-url,omitempty"`
	BackgroundArtifact string `json:"background-artifact,omitempty"`
}

func main() {
	logger.Info("IVCAP Example: Gradient Text Image",
		log.String("gitCommit", GitCommit),
		log.String("gitTag", GitTag),
		log.String("buildDate", BuildDate),
	)

	var (
		msgF              = flag.String("msg", "Hello World", "Message to print on image")
		imgArtF           = flag.String("img-art", "", "URL of artifact to add as background")
		imgUrlF           = flag.String("img-url", "", "URL of external image to add as background")
		localF            = flag.Bool("local", false, "Run in local mode for testing")
		noCachingF        = flag.Bool("no-caching", false, "Do not use the CACHE-URL option if available")
		skipSidecarCheckF = flag.Bool("skip-sidecar-check", false, "Skip checking for a sidecar")
	)
	flag.Parse()

	ivcapEnv = ivcap.NewEnvironment(
		ivcap.NoCaching(*noCachingF),
		ivcap.LocalMode(*localF),
		ivcap.Logger(ivcap.WrapLogger(logger)),
	)

	logger.Info("Parameters:", log.String("msg", *msgF),
		log.String("img-art", *imgArtF), log.String("img-url", *imgUrlF))

	if !*skipSidecarCheckF {
		ivcapEnv.WaitForEnvironmentReady(MAX_ATTEMPTS)
	}

	var img image.Image
	var err error
	if *imgArtF != "" {
		img, err = getImage(*imgArtF)
	} else if *imgUrlF != "" {
		img, err = getImage(*imgUrlF)
	}
	if err != nil {
		logger.Panic("while getting background image", log.Error(err))
	}

	meta := Meta{
		Msg:                *msgF,
		BackgroundUrl:      *imgUrlF,
		BackgroundArtifact: *imgArtF,
	}

	wg := ivcapEnv.PublishAsync("out.png", "image/png", &meta, func(writer *io.PipeWriter) error {
		draw(*msgF, img, ivcapEnv.GetOrderID(), W, H, writer)
		return nil
	})
	// Wait for image to be published
	wg.Wait()
	logger.Info("DONE")
}
