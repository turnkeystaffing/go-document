package document

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRequestJSONRoundTripAllFields(t *testing.T) {
	scale := 1.5
	printBg := true
	paperSize := "A4"
	orientation := "portrait"
	marginTop := "20mm"
	marginBottom := "10mm"
	marginLeft := "15mm"
	marginRight := "15mm"

	req := RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		CustomCSS:   "body { color: red; }",
		Options: &RenderOptions{
			PaperSize:       &paperSize,
			Orientation:     &orientation,
			MarginTop:       &marginTop,
			MarginBottom:    &marginBottom,
			MarginLeft:      &marginLeft,
			MarginRight:     &marginRight,
			Scale:           &scale,
			PrintBackground: &printBg,
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded RenderRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, req.Content, decoded.Content)
	assert.Equal(t, req.ContentType, decoded.ContentType)
	assert.Equal(t, req.Format, decoded.Format)
	assert.Equal(t, req.CustomCSS, decoded.CustomCSS)
	require.NotNil(t, decoded.Options)
	assert.Equal(t, *req.Options.PaperSize, *decoded.Options.PaperSize)
	assert.Equal(t, *req.Options.Orientation, *decoded.Options.Orientation)
	assert.Equal(t, *req.Options.MarginTop, *decoded.Options.MarginTop)
	assert.Equal(t, *req.Options.MarginBottom, *decoded.Options.MarginBottom)
	assert.Equal(t, *req.Options.MarginLeft, *decoded.Options.MarginLeft)
	assert.Equal(t, *req.Options.MarginRight, *decoded.Options.MarginRight)
	assert.Equal(t, *req.Options.Scale, *decoded.Options.Scale)
	assert.Equal(t, *req.Options.PrintBackground, *decoded.Options.PrintBackground)
}

func TestRenderRequestJSONRoundTripMinimalFields(t *testing.T) {
	req := RenderRequest{
		Content:     "# Markdown",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Verify omitempty: custom_css and options should be absent.
	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.NotContains(t, raw, "custom_css", "custom_css should be omitted when empty")
	assert.NotContains(t, raw, "options", "options should be omitted when nil")

	var decoded RenderRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, req.Content, decoded.Content)
	assert.Equal(t, req.ContentType, decoded.ContentType)
	assert.Equal(t, req.Format, decoded.Format)
	assert.Empty(t, decoded.CustomCSS)
	assert.Nil(t, decoded.Options)
}

func TestRenderResultJSONRoundTrip(t *testing.T) {
	result := RenderResult{
		Data:        []byte{0x25, 0x50, 0x44, 0x46}, // %PDF
		ContentType: "application/pdf",
		Metadata: map[string]string{
			MetadataKeyPages:            "3",
			MetadataKeyRenderDurationMs: "150",
			MetadataKeyBlockedResources: "0",
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded RenderResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, result.Data, decoded.Data)
	assert.Equal(t, result.ContentType, decoded.ContentType)
	assert.Equal(t, result.Metadata, decoded.Metadata)
}

func TestRenderResultJSONOmitsEmptyMetadata(t *testing.T) {
	result := RenderResult{
		Data:        []byte{0x25, 0x50, 0x44, 0x46},
		ContentType: "application/pdf",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.NotContains(t, raw, "metadata", "metadata should be omitted when nil")
}

func TestRenderResultJSONEmptyNonNilMetadata(t *testing.T) {
	// Go's omitempty omits both nil and empty (len=0) maps — they produce identical JSON.
	result := RenderResult{
		Data:        []byte{0x25, 0x50, 0x44, 0x46},
		ContentType: "application/pdf",
		Metadata:    map[string]string{},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.NotContains(t, raw, "metadata", "empty non-nil map should be omitted (same as nil)")
}

func TestRenderRequestEmptyOptionsVsNilOptions(t *testing.T) {
	// Verifies semantic difference: Options: nil omits "options" from JSON,
	// while Options: &RenderOptions{} includes "options":{}.
	// The gateway may interpret these differently (absent vs explicit empty).
	reqNil := RenderRequest{
		Content:     "test",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		Options:     nil,
	}
	dataNil, err := json.Marshal(reqNil)
	require.NoError(t, err)

	var rawNil map[string]json.RawMessage
	err = json.Unmarshal(dataNil, &rawNil)
	require.NoError(t, err)
	assert.NotContains(t, rawNil, "options", "nil Options should be omitted from JSON")

	reqEmpty := RenderRequest{
		Content:     "test",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		Options:     &RenderOptions{},
	}
	dataEmpty, err := json.Marshal(reqEmpty)
	require.NoError(t, err)

	var rawEmpty map[string]json.RawMessage
	err = json.Unmarshal(dataEmpty, &rawEmpty)
	require.NoError(t, err)
	assert.Contains(t, rawEmpty, "options", "non-nil empty Options should be present in JSON")
	assert.JSONEq(t, `{}`, string(rawEmpty["options"]))
}

func TestRenderOptionsJSONNilVsPopulated(t *testing.T) {
	// All nil — should produce empty JSON object (all fields omitempty).
	opts := RenderOptions{}
	data, err := json.Marshal(opts)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))

	// Partially populated.
	paperSize := "Letter"
	scale := 0.8
	opts = RenderOptions{
		PaperSize: &paperSize,
		Scale:     &scale,
	}
	data, err = json.Marshal(opts)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Contains(t, raw, "paper_size")
	assert.Contains(t, raw, "scale")
	assert.NotContains(t, raw, "orientation")
	assert.NotContains(t, raw, "margin_top")

	// Round-trip.
	var decoded RenderOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	require.NotNil(t, decoded.PaperSize)
	assert.Equal(t, "Letter", *decoded.PaperSize)
	require.NotNil(t, decoded.Scale)
	assert.InDelta(t, 0.8, *decoded.Scale, 1e-9)
	assert.Nil(t, decoded.Orientation)
	assert.Nil(t, decoded.PrintBackground)
}

func TestJSONFieldNamesMatchAPIContract(t *testing.T) {
	// Verifies that serialized JSON uses the exact field names expected by POST /v1/render.
	scale := 1.0
	printBg := false
	paperSize := "A4"
	orientation := "landscape"
	marginTop := "10mm"
	marginBottom := "10mm"
	marginLeft := "10mm"
	marginRight := "10mm"

	req := RenderRequest{
		Content:     "test",
		ContentType: "text/html",
		Format:      "pdf",
		CustomCSS:   ".x{}",
		Options: &RenderOptions{
			PaperSize:       &paperSize,
			Orientation:     &orientation,
			MarginTop:       &marginTop,
			MarginBottom:    &marginBottom,
			MarginLeft:      &marginLeft,
			MarginRight:     &marginRight,
			Scale:           &scale,
			PrintBackground: &printBg,
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Top-level field names from gateway RenderRequestDTO.
	assert.Contains(t, raw, "content")
	assert.Contains(t, raw, "content_type")
	assert.Contains(t, raw, "format")
	assert.Contains(t, raw, "custom_css")
	assert.Contains(t, raw, "options")

	// Options field names from gateway RenderOptionsDTO.
	var optsRaw map[string]json.RawMessage
	err = json.Unmarshal(raw["options"], &optsRaw)
	require.NoError(t, err)

	assert.Contains(t, optsRaw, "paper_size")
	assert.Contains(t, optsRaw, "orientation")
	assert.Contains(t, optsRaw, "margin_top")
	assert.Contains(t, optsRaw, "margin_bottom")
	assert.Contains(t, optsRaw, "margin_left")
	assert.Contains(t, optsRaw, "margin_right")
	assert.Contains(t, optsRaw, "scale")
	assert.Contains(t, optsRaw, "print_background")
}

func TestRenderResultJSONFieldNamesMatchAPIContract(t *testing.T) {
	// Verifies RenderResult JSON field names match gateway RenderResponseDTO.
	result := RenderResult{
		Data:        []byte{0x25, 0x50, 0x44, 0x46},
		ContentType: "application/pdf",
		Metadata: map[string]string{
			MetadataKeyPages: "1",
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Field names from gateway RenderResponseDTO.
	assert.Contains(t, raw, "data")
	assert.Contains(t, raw, "content_type")
	assert.Contains(t, raw, "metadata")
}

func TestContentTypeConstants(t *testing.T) {
	assert.Equal(t, "text/html", ContentTypeHTML)
	assert.Equal(t, "text/markdown", ContentTypeMarkdown)
	assert.Equal(t, "application/pdf", ContentTypePDF)
}

func TestFormatConstant(t *testing.T) {
	assert.Equal(t, "pdf", FormatPDF)
}

func TestMetadataKeyConstants(t *testing.T) {
	assert.Equal(t, "pages", MetadataKeyPages)
	assert.Equal(t, "render_duration_ms", MetadataKeyRenderDurationMs)
	assert.Equal(t, "blocked_resources", MetadataKeyBlockedResources)
}

func TestRenderRequestZeroValueIsValid(t *testing.T) {
	// AC2: zero-value RenderRequest{} is valid Go (no required constructor).
	var req RenderRequest
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded RenderRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, req, decoded)
}

func TestRenderOptionsZeroValuePointersIncludedInJSON(t *testing.T) {
	// GAP-002: Pointer types with zero values (0.0, false) must serialize because
	// the pointer is non-nil. omitempty only omits nil pointers, not zero-value pointees.
	zeroScale := 0.0
	falseBg := false
	opts := RenderOptions{
		Scale:           &zeroScale,
		PrintBackground: &falseBg,
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Contains(t, raw, "scale", "zero-value Scale pointer must be serialized")
	assert.Contains(t, raw, "print_background", "false PrintBackground pointer must be serialized")

	// Verify actual values survive round-trip.
	var decoded RenderOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	require.NotNil(t, decoded.Scale)
	assert.InDelta(t, 0.0, *decoded.Scale, 1e-15)
	require.NotNil(t, decoded.PrintBackground)
	assert.False(t, *decoded.PrintBackground)
}

func TestRenderOptionsFullRoundTrip(t *testing.T) {
	// GAP-003: Verify all 8 RenderOptions fields survive round-trip.
	paperSize := "Legal"
	orientation := "landscape"
	marginTop := "25mm"
	marginBottom := "15mm"
	marginLeft := "20mm"
	marginRight := "20mm"
	scale := 1.2
	printBg := true

	opts := RenderOptions{
		PaperSize:       &paperSize,
		Orientation:     &orientation,
		MarginTop:       &marginTop,
		MarginBottom:    &marginBottom,
		MarginLeft:      &marginLeft,
		MarginRight:     &marginRight,
		Scale:           &scale,
		PrintBackground: &printBg,
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)

	var decoded RenderOptions
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.NotNil(t, decoded.PaperSize)
	assert.Equal(t, "Legal", *decoded.PaperSize)
	require.NotNil(t, decoded.Orientation)
	assert.Equal(t, "landscape", *decoded.Orientation)
	require.NotNil(t, decoded.MarginTop)
	assert.Equal(t, "25mm", *decoded.MarginTop)
	require.NotNil(t, decoded.MarginBottom)
	assert.Equal(t, "15mm", *decoded.MarginBottom)
	require.NotNil(t, decoded.MarginLeft)
	assert.Equal(t, "20mm", *decoded.MarginLeft)
	require.NotNil(t, decoded.MarginRight)
	assert.Equal(t, "20mm", *decoded.MarginRight)
	require.NotNil(t, decoded.Scale)
	assert.InDelta(t, 1.2, *decoded.Scale, 1e-9)
	require.NotNil(t, decoded.PrintBackground)
	assert.True(t, *decoded.PrintBackground)
}

func TestRenderOptionsScaleNaNCausesMarshalError(t *testing.T) {
	// Documents that NaN/Infinity float64 values cause json.Marshal to error.
	nan := math.NaN()
	opts := RenderOptions{Scale: &nan}
	_, err := json.Marshal(opts)
	assert.Error(t, err, "NaN Scale should cause json.Marshal error")

	inf := math.Inf(1)
	opts = RenderOptions{Scale: &inf}
	_, err = json.Marshal(opts)
	assert.Error(t, err, "Infinity Scale should cause json.Marshal error")
}

func TestRenderRequestFromRawJSON(t *testing.T) {
	// GAP-004: Deserialize from raw JSON matching POST /v1/render format.
	rawJSON := `{
		"content": "<p>Hello World</p>",
		"content_type": "text/html",
		"format": "pdf",
		"custom_css": "p { margin: 0; }",
		"options": {
			"paper_size": "A4",
			"orientation": "portrait",
			"margin_top": "10mm",
			"scale": 1.0,
			"print_background": true
		}
	}`

	var req RenderRequest
	err := json.Unmarshal([]byte(rawJSON), &req)
	require.NoError(t, err)

	assert.Equal(t, "<p>Hello World</p>", req.Content)
	assert.Equal(t, ContentTypeHTML, req.ContentType)
	assert.Equal(t, FormatPDF, req.Format)
	assert.Equal(t, "p { margin: 0; }", req.CustomCSS)
	require.NotNil(t, req.Options)
	require.NotNil(t, req.Options.PaperSize)
	assert.Equal(t, "A4", *req.Options.PaperSize)
	require.NotNil(t, req.Options.Orientation)
	assert.Equal(t, "portrait", *req.Options.Orientation)
	require.NotNil(t, req.Options.MarginTop)
	assert.Equal(t, "10mm", *req.Options.MarginTop)
	require.NotNil(t, req.Options.Scale)
	assert.InDelta(t, 1.0, *req.Options.Scale, 1e-9)
	require.NotNil(t, req.Options.PrintBackground)
	assert.True(t, *req.Options.PrintBackground)
	// Unset fields should remain nil.
	assert.Nil(t, req.Options.MarginBottom)
	assert.Nil(t, req.Options.MarginLeft)
	assert.Nil(t, req.Options.MarginRight)
}

func TestRenderResultFromRawJSON(t *testing.T) {
	// GAP-004: Deserialize RenderResult from SDK-native JSON format.
	// Note: This tests SDK-to-SDK serialization where metadata values are strings.
	// The gateway's RenderResponseMetadata uses typed integer fields (e.g., "pages": 5),
	// NOT string values. Direct deserialization of gateway JSON into RenderResult will
	// fail for metadata — see TestRenderResultGatewayMetadataTypeMismatch.
	// Data is base64-encoded in JSON per Go's []byte marshaling convention.
	// "JVBERg==" is base64 for []byte{0x25, 0x50, 0x44, 0x46} (%PDF).
	rawJSON := `{
		"data": "JVBERg==",
		"content_type": "application/pdf",
		"metadata": {
			"pages": "5",
			"render_duration_ms": "230",
			"blocked_resources": "1"
		}
	}`

	var result RenderResult
	err := json.Unmarshal([]byte(rawJSON), &result)
	require.NoError(t, err)

	assert.Equal(t, []byte{0x25, 0x50, 0x44, 0x46}, result.Data)
	assert.Equal(t, ContentTypePDF, result.ContentType)
	assert.Equal(t, "5", result.Metadata[MetadataKeyPages])
	assert.Equal(t, "230", result.Metadata[MetadataKeyRenderDurationMs])
	assert.Equal(t, "1", result.Metadata[MetadataKeyBlockedResources])
}

func TestRenderResultGatewayMetadataTypeMismatch(t *testing.T) {
	// Documents that gateway JSON (integer metadata) cannot be directly deserialized
	// into RenderResult (map[string]string metadata). The HTTPProvider must manually
	// convert RenderResponseMetadata struct fields to map[string]string.
	gatewayJSON := `{
		"data": "JVBERg==",
		"content_type": "application/pdf",
		"metadata": {
			"pages": 5,
			"render_duration_ms": 230,
			"blocked_resources": 1
		}
	}`

	var result RenderResult
	err := json.Unmarshal([]byte(gatewayJSON), &result)
	// json.Unmarshal fails because integer values cannot be placed into map[string]string.
	assert.Error(t, err, "gateway JSON with integer metadata should fail to unmarshal into map[string]string")
}

func TestRenderResultDataEdgeCases(t *testing.T) {
	// GAP-005: Verify []byte Data edge cases.
	tests := []struct {
		name string
		data []byte
	}{
		{"nil_data", nil},
		{"empty_data", []byte{}},
		{"single_byte", []byte{0xFF}},
		{"binary_content", []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderResult{
				Data:        tt.data,
				ContentType: ContentTypePDF,
			}

			encoded, err := json.Marshal(result)
			require.NoError(t, err)

			var decoded RenderResult
			err = json.Unmarshal(encoded, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.data, decoded.Data)
			assert.Equal(t, ContentTypePDF, decoded.ContentType)
		})
	}
}

func TestRenderResultMetadataArbitraryKeys(t *testing.T) {
	// GAP-007: Metadata supports arbitrary keys beyond well-known constants.
	result := RenderResult{
		Data:        []byte{0x25, 0x50, 0x44, 0x46},
		ContentType: ContentTypePDF,
		Metadata: map[string]string{
			MetadataKeyPages:         "2",
			"custom_key":             "custom_value",
			"render_engine_version":  "1.5.0",
			"key_with_special_chars": "value with spaces & symbols!",
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded RenderResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Len(t, decoded.Metadata, 4)
	assert.Equal(t, "2", decoded.Metadata[MetadataKeyPages])
	assert.Equal(t, "custom_value", decoded.Metadata["custom_key"])
	assert.Equal(t, "1.5.0", decoded.Metadata["render_engine_version"])
	assert.Equal(t, "value with spaces & symbols!", decoded.Metadata["key_with_special_chars"])
}

func TestRenderRequestMalformedFieldTypes(t *testing.T) {
	// Verifies that wrong JSON types for known fields cause unmarshal errors.
	tests := []struct {
		name    string
		rawJSON string
	}{
		{"integer_content", `{"content": 123, "content_type": "text/html", "format": "pdf"}`},
		{"object_content_type", `{"content": "x", "content_type": {"nested": true}, "format": "pdf"}`},
		{"array_format", `{"content": "x", "content_type": "text/html", "format": ["pdf"]}`},
		{"string_options", `{"content": "x", "content_type": "text/html", "format": "pdf", "options": "not_an_object"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req RenderRequest
			err := json.Unmarshal([]byte(tt.rawJSON), &req)
			assert.Error(t, err, "wrong JSON type should cause unmarshal error")
		})
	}
}

func TestUnmarshalUnknownFieldsIgnored(t *testing.T) {
	// Verify that unknown JSON fields don't cause errors (forward compatibility).
	rawJSON := `{
		"content": "test",
		"content_type": "text/html",
		"format": "pdf",
		"unknown_field": "should be ignored",
		"another_future_field": 42
	}`

	var req RenderRequest
	err := json.Unmarshal([]byte(rawJSON), &req)
	require.NoError(t, err)
	assert.Equal(t, "test", req.Content)
	assert.Equal(t, ContentTypeHTML, req.ContentType)
	assert.Equal(t, FormatPDF, req.Format)
}
