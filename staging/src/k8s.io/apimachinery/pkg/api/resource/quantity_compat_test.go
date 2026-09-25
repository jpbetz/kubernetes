/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cbor "k8s.io/apimachinery/pkg/runtime/serializer/cbor/direct"
)

func TestQuantityOutOfInt32ExponentCompatibility(t *testing.T) {
	testCases := []struct {
		in          string
		wantValue   int64
		wantFitsI64 bool
		wantString  string
	}{
		{in: "1e4294967296", wantValue: 1, wantFitsI64: true, wantString: "1e4294967296"},
		{in: "1e4294967297", wantValue: 10, wantFitsI64: true, wantString: "10"},
		{in: "-1e4294967296", wantValue: -1, wantFitsI64: true, wantString: "-1e4294967296"},
		{in: "1e-4294967296", wantValue: 1, wantFitsI64: true, wantString: "1e-4294967296"},
		{in: "1e8589934592", wantValue: 1, wantFitsI64: true, wantString: "1e8589934592"},

		// Fraction digits move the scale off the exponent, so these are the
		// spellings at 2147483648 that a <=1.37 apiserver could write. None
		// of them fits an int64, so only the spelling has to survive.
		{in: "1.25e2147483648", wantString: "1.25e2147483648"},
		{in: "1.5e2147483648", wantString: "150e2147483646"},
		{in: "1.25e-2147483648", wantString: "1.25e-2147483648"},
		{in: "0.5e2147483648", wantString: "50e2147483646"},
	}

	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			q, err := ParseQuantity(tc.in)
			if err != nil {
				t.Fatalf("ParseQuantity(%q) failed: %v", tc.in, err)
			}
			// Only the flag is portable; the value returned alongside a
			// false has changed between releases.
			if got, ok := q.AsInt64(); ok != tc.wantFitsI64 || (ok && got != tc.wantValue) {
				t.Errorf("AsInt64() = (%d, %v), want (%d, %v)", got, ok, tc.wantValue, tc.wantFitsI64)
			}
			if got := q.String(); got != tc.wantString {
				t.Errorf("String() = %q, want %q", got, tc.wantString)
			}

			// Decoding is the path that matters: the spelling below is what a
			// 1.37 apiserver wrote into etcd.
			var fromJSON Quantity
			if err := json.Unmarshal([]byte(strconv.Quote(tc.in)), &fromJSON); err != nil {
				t.Fatalf("json.Unmarshal(%q) failed: %v", tc.in, err)
			}
			if got, ok := fromJSON.AsInt64(); ok != tc.wantFitsI64 || (ok && got != tc.wantValue) {
				t.Errorf("after json decode: AsInt64() = (%d, %v), want (%d, %v)", got, ok, tc.wantValue, tc.wantFitsI64)
			}

			// Re-encoding must not rewrite what is already stored.
			data, err := json.Marshal(&fromJSON)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if got, want := string(data), strconv.Quote(tc.wantString); got != want {
				t.Errorf("json.Marshal = %s, want %s", got, want)
			}

			// 1.37 stored the same spelling through protobuf.
			buf, err := q.Marshal()
			if err != nil {
				t.Fatalf("proto Marshal failed: %v", err)
			}
			var fromProto Quantity
			if err := fromProto.Unmarshal(buf); err != nil {
				t.Fatalf("proto Unmarshal failed: %v", err)
			}
			if got, ok := fromProto.AsInt64(); ok != tc.wantFitsI64 || (ok && got != tc.wantValue) {
				t.Errorf("after proto decode: AsInt64() = (%d, %v), want (%d, %v)", got, ok, tc.wantValue, tc.wantFitsI64)
			}
		})
	}
}

func TestQuantityPriorReleaseCompatibility(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "quantities-*.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no testdata/quantities-*.tsv files found")
	}
	for _, path := range paths {
		release := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "quantities-"), ".tsv")
		rows := readQuantityCorpus(t, path)
		for _, format := range quantityWireFormats {
			t.Run(release+"/"+format.name, func(t *testing.T) {
				for _, row := range rows {
					stored, err := format.wrap(row.stored)
					if err != nil {
						t.Fatal(err)
					}
					var q Quantity
					if err := format.decode(&q, stored); err != nil {
						t.Errorf("decode %q: %v; %s decoded it to %s", row.stored, err, release, row.value)
						continue
					}
					if got := exactDecimal(q); got != row.value {
						t.Errorf("%q decodes to %s; %s decoded it to %s", row.stored, got, release, row.value)
					}
					if row.reencoded == "" {
						continue
					}
					got, err := format.encode(&q)
					if err != nil {
						t.Errorf("encode %q: %v", row.stored, err)
						continue
					}
					want, err := format.wrap(row.reencoded)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Errorf("%q re-encodes as %q, want %q; if the new spelling is intended, update its reencoded column in %s", row.stored, got, want, path)
					}
				}
			})
		}
	}
}

var quantityWireFormats = []struct {
	name   string
	wrap   func(string) ([]byte, error)
	decode func(*Quantity, []byte) error
	encode func(*Quantity) ([]byte, error)
}{
	{
		name:   "JSON",
		wrap:   func(s string) ([]byte, error) { return json.Marshal(s) },
		decode: (*Quantity).UnmarshalJSON,
		encode: (*Quantity).MarshalJSON,
	},
	{
		name:   "CBOR",
		wrap:   func(s string) ([]byte, error) { return cbor.Marshal(s) },
		decode: (*Quantity).UnmarshalCBOR,
		encode: (*Quantity).MarshalCBOR,
	},
	{
		name: "protobuf",
		wrap: func(s string) ([]byte, error) {
			return append(binary.AppendUvarint([]byte{0x0a}, uint64(len(s))), s...), nil
		},
		decode: (*Quantity).Unmarshal,
		encode: (*Quantity).Marshal,
	},
}

type quantityCorpusRow struct {
	stored, reencoded, value string
}

var quantityCorpusHeader = []string{"stored", "reencoded", "value"}

func readQuantityCorpus(t *testing.T, path string) []quantityCorpusRow {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '\t'
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(records) < 2 || !slices.Equal(records[0], quantityCorpusHeader) {
		t.Fatalf("%s: want a %q header followed by rows", path, quantityCorpusHeader)
	}
	rows := make([]quantityCorpusRow, 0, len(records)-1)
	for _, record := range records[1:] {
		rows = append(rows, quantityCorpusRow{stored: record[0], reencoded: record[1], value: record[2]})
	}
	return rows
}

func exactDecimal(q Quantity) string {
	d := q.AsDec()
	unscaled, exponent := d.UnscaledBig(), -int64(d.Scale())
	if unscaled.Sign() == 0 {
		return "0"
	}
	ten := big.NewInt(10)
	for {
		quotient, remainder := new(big.Int).QuoRem(unscaled, ten, new(big.Int))
		if remainder.Sign() != 0 {
			return fmt.Sprintf("%se%d", unscaled, exponent)
		}
		unscaled, exponent = quotient, exponent+1
	}
}

func TestGenerateQuantityReleaseCorpus(t *testing.T) {
	if spelling := os.Getenv("QUANTITY_CORPUS_PROBE"); spelling != "" {
		printQuantityProbe(spelling)
		return
	}
	path := os.Getenv("QUANTITY_RELEASE_CORPUS")
	if path == "" {
		t.Skip("set QUANTITY_RELEASE_CORPUS to the testdata/quantities-<release>.tsv path to write for the checked-out release")
	}
	seen := map[string]bool{}
	var pending []string
	enqueue := func(spelling string) {
		if spelling != "" && !seen[spelling] {
			seen[spelling] = true
			pending = append(pending, spelling)
		}
	}
	for _, spelling := range quantityCorpusInputs() {
		enqueue(spelling)
	}
	rows := map[string]quantityCorpusRow{}
	for len(pending) > 0 {
		batch := pending
		pending = nil
		for i, probe := range probeQuantities(t, batch) {
			if probe.value == "" {
				continue
			}
			rows[batch[i]] = quantityCorpusRow{stored: batch[i], reencoded: probe.reencoded, value: probe.value}
			enqueue(probe.reencoded)
			enqueue(probe.canonical)
		}
	}
	records := [][]string{quantityCorpusHeader}
	for _, stored := range slices.Sorted(maps.Keys(rows)) {
		row := rows[stored]
		if rows[row.reencoded].value != row.value {
			row.reencoded = ""
		}
		records = append(records, []string{row.stored, row.reencoded, row.value})
	}
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	w.Comma = '\t'
	if err := w.WriteAll(records); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d rows to %s", len(rows), path)
}

func quantityCorpusInputs() []string {
	mantissas := []string{
		"0", "01", "1", "5", "12", "999", "1000", "1024", "123456789",
		"999999999999999999", "1000000000000000000", "9223372036854775", "9223372036854776",
		"9223372036854775807", "9223372036854775808", "18446744073709551615", "18446744073709551616",
		"123456789012345678901", "1234567890123456789012345678901234567890",
		".5", "1.", "0.1", "0.5", "1.5", "1.25", "0.001", "0.000000001", "0.0000000001", "1.0000000001",
		"123.456789012", "9223372036854775.807", "9223372036854775.808",
	}
	suffixes := []string{"", "n", "u", "m", "k", "M", "G", "T", "P", "E", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "E3", "e+21", "e03"}
	for _, exponent := range []string{
		"-4294967297", "-4294967296", "-2147483649", "-2147483648", "-2147483647", "-2147483646", "-2147483000",
		"-1000", "-330", "-19", "-18", "-10", "-9", "-3", "-1", "0", "1", "3", "9", "10", "18", "19", "21", "330", "1000",
		"2147483000", "2147483627", "2147483646", "2147483647", "2147483648", "4294967295", "4294967296", "4294967297", "8589934592",
	} {
		suffixes = append(suffixes, "e"+exponent)
	}
	var inputs []string
	for _, sign := range []string{"", "-", "+"} {
		for _, mantissa := range mantissas {
			for _, suffix := range suffixes {
				inputs = append(inputs, sign+mantissa+suffix)
			}
		}
	}
	return inputs
}

type quantityProbe struct {
	value, reencoded, canonical string
}

func printQuantityProbe(spelling string) {
	q, err := ParseQuantity(spelling)
	if err != nil {
		return
	}
	fmt.Printf("value\t%s\t%s\n", exactDecimal(q), q.String())
	canonical := q.DeepCopy()
	canonical.Add(*NewQuantity(0, DecimalSI))
	fmt.Printf("canonical\t%s\n", canonical.String())
}

func probeQuantities(t *testing.T, spellings []string) []quantityProbe {
	probes := make([]quantityProbe, len(spellings))
	workers := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, spelling := range spellings {
		workers <- struct{}{}
		wg.Go(func() {
			defer func() { <-workers }()
			probes[i] = probeQuantityInChildProcess(t, spelling)
		})
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	return probes
}

func probeQuantityInChildProcess(t *testing.T, spelling string) quantityProbe {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGenerateQuantityReleaseCorpus$")
	cmd.Env = append(os.Environ(), "QUANTITY_CORPUS_PROBE="+spelling)
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Errorf("probing %q: %v", spelling, err)
	}
	var probe quantityProbe
	for line := range strings.Lines(string(out)) {
		switch fields := strings.Fields(line); {
		case len(fields) == 3 && fields[0] == "value":
			probe.value, probe.reencoded = fields[1], fields[2]
		case len(fields) == 2 && fields[0] == "canonical":
			probe.canonical = fields[1]
		}
	}
	return probe
}
