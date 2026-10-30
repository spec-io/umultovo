package main

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseRCNArchives(t *testing.T) {
	page := `<a href="/download/119/9590/BazaDanychRCNPoznan20261.zip">
<strong>Baza Danych RCN Poznań 2026</strong><span>zip, 1.48 MB 26.10.2026</span></a>
<a href="https://example.com/download/fake.zip"><strong>Baza Danych RCN Poznań 2027</strong></a>`
	archives, err := parseRCNArchives(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("got %d archives, want 1", len(archives))
	}
	archive := archives[0]
	if archive.Name != "Baza Danych RCN Poznań 2026" ||
		archive.URL != "https://bip.geopoz.poznan.pl/download/119/9590/BazaDanychRCNPoznan20261.zip" ||
		archive.Info != "zip, 1.48 MB 26.10.2026" || len(archive.ID) != 16 {
		t.Fatalf("unexpected archive metadata: %#v", archive)
	}
}

func TestRCNArchiveFreshnessUsesRemoteMetadata(t *testing.T) {
	local := downloadedRCNArchive{Info: "zip, 1.48 MB 26.10.2026", ETag: `"old"`}
	remote := rcnArchive{Info: "zip, 1.50 MB 30.10.2026"}
	if !archiveMetadataChanged(remote, local) {
		t.Fatal("changed GEOPOZ listing info should signal a newer archive")
	}
	remote.Info = local.Info
	if archiveMetadataChanged(remote, local) {
		t.Fatal("matching GEOPOZ listing info should not signal an update")
	}
}

func TestStageRCNGMLFilesRejectsTraversalAndExtractsGML(t *testing.T) {
	directory := t.TempDir()
	var archiveBytes bytes.Buffer
	writer := zip.NewWriter(&archiveBytes)
	entry, err := writer.Create("Baza Danych RCN Poznań 2026.gml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("<gml/>")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archiveBytes.Bytes()), int64(archiveBytes.Len()))
	if err != nil {
		t.Fatal(err)
	}
	staged, err := stageRCNGMLFiles(directory, reader.File)
	if err != nil {
		t.Fatal(err)
	}
	defer removeStagedRCNFiles(staged)
	if len(staged) != 1 || staged[0].name != "Baza Danych RCN Poznań 2026.gml" {
		t.Fatalf("unexpected staged files: %#v", staged)
	}
	content, err := os.ReadFile(staged[0].temporary)
	if err != nil || string(content) != "<gml/>" {
		t.Fatalf("staged file contents = %q, err = %v", content, err)
	}

	archiveBytes.Reset()
	writer = zip.NewWriter(&archiveBytes)
	entry, err = writer.Create("../escape.gml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err = zip.NewReader(bytes.NewReader(archiveBytes.Bytes()), int64(archiveBytes.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stageRCNGMLFiles(directory, reader.File); err == nil {
		t.Fatal("path traversal archive entry should be rejected")
	}
	if _, err := os.Stat(directory + "/escape.gml"); !os.IsNotExist(err) {
		t.Fatalf("traversal destination exists or could not be checked: %v", err)
	}
}

func TestDecodeFeaturesExtractsFieldsReferencesAnd2177Coordinates(t *testing.T) {
	input := `<root xmlns:rcn="urn:rcn" xmlns:gml="urn:gml" xmlns:xlink="urn:xlink"><member>
<rcn:RCN_Dzialka gml:id="parcel_2025-01-02T03-04-05"><rcn:idDzialki>parcel-1</rcn:idDzialki>
<rcn:geometria><gml:posList>5815000 6429000 5815010 6429010 5815000 6429000</gml:posList></rcn:geometria>
<rcn:nieruchomosc xlink:href="#property_2025-01-02T03-04-05"/></rcn:RCN_Dzialka>
</member></root>`

	var got []feature
	count, err := decodeFeatures(strings.NewReader(input), "test.gml", func(f feature) {
		got = append(got, f)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(got) != 1 {
		t.Fatalf("got %d decoded features, want 1", count)
	}
	f := got[0]
	if f.Type != "RCN_Dzialka" || f.ID != "parcel" || f.Fields["idDzialki"] != "parcel-1" {
		t.Fatalf("unexpected feature fields: %#v", f)
	}
	if f.Source != "test.gml" || len(f.Points) != 3 || f.Points[0] != (point{X: 6429000, Y: 5815000}) {
		t.Fatalf("unexpected geometry/source: %#v", f)
	}
	if len(f.Refs) != 1 || f.Refs[0] != (reference{Kind: "nieruchomosc", ID: "property"}) {
		t.Fatalf("unexpected references: %#v", f.Refs)
	}
}

func TestPolygonCentroidAndDistance(t *testing.T) {
	polygon := []point{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}}
	center, ok := polygonCentroid(polygon)
	if !ok || center != (point{5, 5}) {
		t.Fatalf("centroid = %#v, %v; want (5,5), true", center, ok)
	}
	if distance := distanceToPolygon(center, polygon); distance != 0 {
		t.Fatalf("interior distance = %v, want 0", distance)
	}
	if distance := distanceToPolygon(point{15, 5}, polygon); mathAbs(distance-5) > 1e-9 {
		t.Fatalf("exterior distance = %v, want 5", distance)
	}
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func TestParseShare(t *testing.T) {
	numerator, denominator, ok := parseShare("3/4")
	if !ok || numerator != 3 || denominator != 4 {
		t.Fatalf("parseShare = %v, %v, %v", numerator, denominator, ok)
	}
	if _, _, ok := parseShare("1/0"); ok {
		t.Fatal("zero denominator should not be accepted")
	}
}
