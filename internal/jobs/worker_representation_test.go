package jobs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/representation/representationtest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func mediaPipes(name string, ss ...pipeline.PipelineStep) pipeline.Registry {
	reg := pipeline.NewRegistry()
	reg.Register(name, &pipeline.Pipeline{PipelineName: name, Steps: ss})
	return reg
}

// A binary image capture: the worker stores the original as artifact/0, the
// OCR and vision steps derive from it.
func TestWorkerImageRepresentationsDeriveFromOriginal(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	reg := mediaPipes("test.image",
		steps.NewOCRExtractor(steps.WithOCRProvider(representationtest.OCR{})),
		steps.NewVisionAnalyzer(steps.WithVisionProvider(representationtest.Vision{})),
	)
	obj := runBinaryJobIn(t, reg, drv, "rep-img", originalPNG, "test.image", "/tmp/cat.png")

	representationtest.RequireProvenance(t, obj.ID, obj.Graph)
	require.True(t, representation.Has(obj.Graph, representation.OCR, representation.Caption))
}

// A binary audio capture: transcript nodes derive from the original and the
// text is indexed once.
func TestWorkerAudioRepresentationsDeriveFromOriginal(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	reg := mediaPipes("test.audio",
		steps.NewAudioTranscriber(steps.WithTranscriptionProvider(representationtest.Transcriber{})),
		steps.NewTimestampAligner(),
		steps.NewSectioner(),
	)
	obj := runBinaryJobIn(t, reg, drv, "rep-aud", []byte("ID3\x00\xff\xfe\x80 not text"), "test.audio", "/tmp/a.mp3")

	representationtest.RequireProvenance(t, obj.ID, obj.Graph)
	require.Equal(t, "first words\nsecond words", projection.ProjectIndex(obj).EmbeddingText)
}
