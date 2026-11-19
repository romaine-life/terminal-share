package store

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// A stored record is the 1 MiB body plus a few fields; anything far past that isn't ours.
const maxRecord = 4 << 20

// Blob keeps each snapshot as the blob <id>.json in one container. The container's
// lifecycle rule (tofu/storage.tf) deletes blobs 30 days after they were created.
type Blob struct {
	container *container.Client
}

// NewBlob opens the container at accountURL (https://<account>.blob.core.windows.net/)
// with cred, the pod's workload identity.
func NewBlob(accountURL, containerName string, cred azcore.TokenCredential) (*Blob, error) {
	client, err := container.NewClient(strings.TrimRight(accountURL, "/")+"/"+containerName, cred, nil)
	if err != nil {
		return nil, err
	}
	return &Blob{container: client}, nil
}

// Put creates the blob, and fails rather than replace one that exists.
func (b *Blob) Put(ctx context.Context, id string, record []byte) error {
	_, err := b.container.NewBlockBlobClient(id+".json").Upload(ctx, streaming.NopCloser(bytes.NewReader(record)), &blockblob.UploadOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: to.Ptr("application/json")},
		AccessConditions: &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{
			IfNoneMatch: to.Ptr(azcore.ETagAny),
		}},
	})
	return err
}

func (b *Blob) Get(ctx context.Context, id string) ([]byte, error) {
	resp, err := b.container.NewBlobClient(id+".json").DownloadStream(ctx, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxRecord))
}
