package lnwire

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/lightningnetwork/lnd/fn"
	"github.com/lightningnetwork/lnd/tlv"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
	"pgregory.net/rapid"
)

// TestExtraOpaqueDataEncodeDecode tests that we're able to encode/decode
// arbitrary payloads.
func TestExtraOpaqueDataEncodeDecode(t *testing.T) {
	t.Parallel()

	type testCase struct {
		// emptyBytes indicates if we should try to encode empty bytes
		// or not.
		emptyBytes bool

		// inputBytes if emptyBytes is false, then we'll read in this
		// set of bytes instead.
		inputBytes []byte
	}

	// We should be able to read in an arbitrary set of bytes as an
	// ExtraOpaqueData, then encode those new bytes into a new instance.
	// The final two instances should be identical.
	scenario := func(test testCase) bool {
		var (
			extraData ExtraOpaqueData
			b         bytes.Buffer
		)

		copy(extraData[:], test.inputBytes)

		if err := extraData.Encode(&b); err != nil {
			t.Fatalf("unable to encode extra data: %v", err)
			return false
		}

		var newBytes ExtraOpaqueData
		if err := newBytes.Decode(&b); err != nil {
			t.Fatalf("unable to decode extra bytes: %v", err)
			return false
		}

		if !bytes.Equal(extraData[:], newBytes[:]) {
			t.Fatalf("expected %x, got %x", extraData,
				newBytes)
			return false
		}

		return true
	}

	// We'll make a function to generate random test data. Half of the
	// time, we'll actually feed in blank bytes.
	quickCfg := &quick.Config{
		Values: func(v []reflect.Value, r *rand.Rand) {
			var newTestCase testCase
			if r.Int31()%2 == 0 {
				newTestCase.emptyBytes = true
			}

			if !newTestCase.emptyBytes {
				numBytes := r.Int31n(1000)
				newTestCase.inputBytes = make([]byte, numBytes)

				_, err := r.Read(newTestCase.inputBytes)
				if err != nil {
					t.Fatalf("unable to gen random bytes: %v", err)
					return
				}
			}

			v[0] = reflect.ValueOf(newTestCase)
		},
	}

	if err := quick.Check(scenario, quickCfg); err != nil {
		t.Fatalf("encode+decode test failed: %v", err)
	}
}

// TestExtraOpaqueDataPackUnpackRecords tests that we're able to pack a set of
// tlv.Records into a stream, and unpack them on the other side to obtain the
// same set of records.
func TestExtraOpaqueDataPackUnpackRecords(t *testing.T) {
	t.Parallel()

	var (
		type1 tlv.Type = 1
		type2 tlv.Type = 2

		channelType1 uint8 = 2
		channelType2 uint8

		hop1 uint32 = 99
		hop2 uint32
	)
	testRecordsProducers := []tlv.RecordProducer{
		&recordProducer{tlv.MakePrimitiveRecord(type1, &channelType1)},
		&recordProducer{tlv.MakePrimitiveRecord(type2, &hop1)},
	}

	// Now that we have our set of sample records and types, we'll encode
	// them into the passed ExtraOpaqueData instance.
	var extraBytes ExtraOpaqueData
	if err := extraBytes.PackRecords(testRecordsProducers...); err != nil {
		t.Fatalf("unable to pack records: %v", err)
	}

	// We'll now simulate decoding these types _back_ into records on the
	// other side.
	newRecords := []tlv.RecordProducer{
		&recordProducer{tlv.MakePrimitiveRecord(type1, &channelType2)},
		&recordProducer{tlv.MakePrimitiveRecord(type2, &hop2)},
	}
	typeMap, err := extraBytes.ExtractRecords(newRecords...)
	require.NoError(t, err, "unable to extract record")

	// We should find that the new backing values have been populated with
	// the proper value.
	switch {
	case channelType1 != channelType2:
		t.Fatalf("wrong record for channel type: expected %v, got %v",
			channelType1, channelType2)

	case hop1 != hop2:
		t.Fatalf("wrong record for hop: expected %v, got %v", hop1,
			hop2)
	}

	// Both types we created above should be found in the type map.
	if _, ok := typeMap[type1]; !ok {
		t.Fatalf("type1 not found in typeMap")
	}
	if _, ok := typeMap[type2]; !ok {
		t.Fatalf("type2 not found in typeMap")
	}
}

// TestPackRecords tests that we're able to pack a set of records into an
// ExtraOpaqueData instance, and then extract them back out. Crucially, we'll
// ensure that records can be packed in any order, and we'll ensure that the
// unpacked records are valid.
func TestPackRecords(t *testing.T) {
	t.Parallel()

	// Create an empty ExtraOpaqueData instance.
	extraBytes := ExtraOpaqueData{}

	var (
		// Record type 1.
		tlvType1     tlv.TlvType1
		recordBytes1 = []byte("recordBytes1")
		tlvRecord1   = tlv.NewPrimitiveRecord[tlv.TlvType1](
			recordBytes1,
		)

		// Record type 2.
		tlvType2     tlv.TlvType2
		recordBytes2 = []byte("recordBytes2")
		tlvRecord2   = tlv.NewPrimitiveRecord[tlv.TlvType2](
			recordBytes2,
		)

		// Record type 3.
		tlvType3     tlv.TlvType3
		recordBytes3 = []byte("recordBytes3")
		tlvRecord3   = tlv.NewPrimitiveRecord[tlv.TlvType3](
			recordBytes3,
		)
	)

	// Pack records 1 and 2 into the ExtraOpaqueData instance.
	err := extraBytes.PackRecords(
		[]tlv.RecordProducer{&tlvRecord1, &tlvRecord2}...,
	)
	require.NoError(t, err)

	// Examine the records that were packed into the ExtraOpaqueData.
	extractedRecords, err := extraBytes.ExtractRecords()
	require.NoError(t, err)

	require.Equal(t, 2, len(extractedRecords))
	require.Equal(t, recordBytes1, extractedRecords[tlvType1.TypeVal()])
	require.Equal(t, recordBytes2, extractedRecords[tlvType2.TypeVal()])

	// Pack records 1, 2, and 3 into the ExtraOpaqueData instance.
	err = extraBytes.PackRecords(
		[]tlv.RecordProducer{&tlvRecord3, &tlvRecord1, &tlvRecord2}...,
	)
	require.NoError(t, err)

	// Examine the records that were packed into the ExtraOpaqueData.
	extractedRecords, err = extraBytes.ExtractRecords()
	require.NoError(t, err)

	require.Equal(t, 3, len(extractedRecords))
	require.Equal(t, recordBytes1, extractedRecords[tlvType1.TypeVal()])
	require.Equal(t, recordBytes2, extractedRecords[tlvType2.TypeVal()])
	require.Equal(t, recordBytes3, extractedRecords[tlvType3.TypeVal()])
}

// TestNewWireTlvMap tests the newWireTlvMap function using property-based
// testing.
func TestNewWireTlvMap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Make a random type map, using the generic Make which'll
		// figure out what type to generate.
		tlvTypeMap := rapid.Make[tlv.TypeMap]().Draw(t, "typeMap")

		// Create a wireTlvMap from the generated type map, this'll
		// operate on our random input.
		result := newWireTlvMap(tlvTypeMap)

		// Property 1: The sum of lengths of officialTypes and
		// customTypes should equal the length of the input typeMap.
		require.Equal(t, len(tlvTypeMap), result.Len())

		// Property 2: All types in customTypes should be >=
		// MinCustomRecordsTlvType.
		require.True(t, fn.All(func(k tlv.Type) bool {
			return uint64(k) >= uint64(MinCustomRecordsTlvType)
		}, maps.Keys(result.customTypes)))

		// Property 3: All types in officialTypes should be <
		// MinCustomRecordsTlvType.
		require.True(t, fn.All(func(k tlv.Type) bool {
			return uint64(k) < uint64(MinCustomRecordsTlvType)
		}, maps.Keys(result.officialTypes)))

		// Property 4: The union of officialTypes and customTypes
		// should equal the input typeMap.
		unionMap := make(tlv.TypeMap)
		maps.Copy(unionMap, result.officialTypes)
		maps.Copy(unionMap, result.customTypes)
		require.Equal(t, tlvTypeMap, unionMap)

		// Property 5: No type should appear in both officialTypes and
		// customTypes.
		require.True(t, fn.All(func(k tlv.Type) bool {
			_, exists := result.officialTypes[k]
			return !exists
		}, maps.Keys(result.customTypes)))
	})
}
