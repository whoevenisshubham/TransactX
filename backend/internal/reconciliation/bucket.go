package reconciliation

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const bucketHeader = "TXBUCKET|v1|"

// BucketID identifies a deterministic UTC time window and logical partition.
// Width is explicit so callers cannot silently change the bucket duration.
type BucketID struct {
	Partition string
	Start     time.Time
	Width     time.Duration
}

func NewBucketID(partition string, start time.Time, width time.Duration) (BucketID, error) {
	bucket := BucketID{Partition: partition, Start: start.UTC(), Width: width}
	if err := bucket.Validate(); err != nil {
		return BucketID{}, err
	}
	return bucket, nil
}

func (bucket BucketID) Validate() error {
	if bucket.Partition == "" {
		return errors.New("bucket partition is required")
	}
	if bucket.Width <= 0 {
		return errors.New("bucket width must be positive")
	}
	return nil
}

// Bytes is the versioned deterministic identity representation:
// TXBUCKET|v1| + length-prefixed partition + UTC RFC3339Nano start + decimal
// duration in nanoseconds. It excludes random UUIDs and physical row IDs.
func (bucket BucketID) Bytes() ([]byte, error) {
	if err := bucket.Validate(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString(bucketHeader)
	writeLengthPrefixed(&out, bucket.Partition)
	writeLengthPrefixed(&out, bucket.Start.UTC().Format(time.RFC3339Nano))
	writeLengthPrefixed(&out, strconv.FormatInt(int64(bucket.Width), 10))
	return out.Bytes(), nil
}

func (bucket BucketID) String() string {
	encoded, err := bucket.Bytes()
	if err != nil {
		return fmt.Sprintf("invalid-bucket:%s:%s:%d", bucket.Partition, bucket.Start.UTC().Format(time.RFC3339Nano), bucket.Width)
	}
	return string(encoded)
}

func (bucket BucketID) Equal(other BucketID) bool {
	left, leftErr := bucket.Bytes()
	right, rightErr := other.Bytes()
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

// Compare orders bucket identities by UTC start, partition, width, and the
// versioned serialized identity as a final deterministic tie-break.
func (bucket BucketID) Compare(other BucketID) int {
	leftStart, rightStart := bucket.Start.UTC(), other.Start.UTC()
	if leftStart.Before(rightStart) {
		return -1
	}
	if leftStart.After(rightStart) {
		return 1
	}
	if bucket.Partition < other.Partition {
		return -1
	}
	if bucket.Partition > other.Partition {
		return 1
	}
	if bucket.Width < other.Width {
		return -1
	}
	if bucket.Width > other.Width {
		return 1
	}
	return bytes.Compare([]byte(bucket.String()), []byte(other.String()))
}

func writeLengthPrefixed(out *bytes.Buffer, value string) {
	out.WriteString(strconv.Itoa(len(value)))
	out.WriteByte(':')
	out.WriteString(value)
}
