// Package files defines REST API /api/files.
package files

import (
	"context"
	"image"
	"image/color"
	_ "image/gif" // Register gif encoder.
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cienet/ldsgo/config"
	"github.com/cienet/ldsgo/gcp/bucket"
	"github.com/cienet/ldsgo/gcp/firestore"
	"github.com/disintegration/imaging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/exp/slices"
	_ "golang.org/x/image/webp" // Register webp encoder.
)

const thumbnailWidth int = 300
const thumbnailHeight int = 300
const pageSize int = 50

// FileMeta the response json of FileMeta.
type FileMeta struct {
	ID         string   `json:"id" binding:"required"`
	Name       string   `json:"name" binding:"required"`
	Tags       []string `json:"tags" binding:"required"`
	URL        string   `json:"url" binding:"required"`
	ThumbURL   string   `json:"thumbUrl" binding:"required"`
	OrderNo    string   `json:"orderNo" binding:"required"`
	FileSize   int64    `json:"size" binding:"required"`
	CreateTime string   `json:"createTime" binding:"required"`
	UpdateTime string   `json:"updateTime" binding:"required"`
}

// FileUploadRequest the request form data of file uploading.
type FileUploadRequest struct {
	Files []*multipart.FileHeader `form:"files" binding:"required"`
	Tags  []string                `form:"tags" binding:"required"`
}

// FileUpdateRequest the request form data of file updating.
type FileUpdateRequest struct {
	File []*multipart.FileHeader `form:"file"`
	Tags []string                `form:"tags" binding:"required"`
}

// FileUpdateResponse the response json of file updating.
type FileUpdateResponse struct {
	File FileMeta `json:"file" binding:"required"`
}

// FileListResponse the response json of file listing.
type FileListResponse struct {
	Files []FileMeta `json:"files" binding:"required"`
}

var imageTypes = []string{".jpg", ".jpeg", ".png", ".gif"}

func toThumbnailPath(path string) string {
	return path + "_small"
}

func toBucketPath(id string) string {
	return config.Config.BucketBasePath + id
}

func toResourceURL(path string) string {
	return config.Config.ResourceBasePath + path
}

func getOrderNo(id string) string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10) + "-" + id
}

func parseTags(tags string) []string {
	if tags == "" {
		return []string{}
	}
	return strings.Fields(strings.ToLower(tags))
}

func parsePageSize(sizeParam string) (int, error) {
	if sizeParam == "" {
		return pageSize, nil
	}
	return strconv.Atoi(sizeParam)
}

// writeFileToBucket uploads file to cloud storage bucket.
func writeFileToBucket(ctx context.Context, client *storage.Client, path string, file *multipart.FileHeader, transcoder bucket.Transcoder) (size int64, err error) {
	defer func() {
		if err != nil {
			log.Printf("Fail to upload file: %s to path: %s error: %s", file.Filename, path, err)
		}
	}()

	f, err := file.Open()
	if err != nil {
		return -1, err
	}
	defer f.Close() // nolint: errcheck

	if transcoder != nil {
		size, err = bucket.TransWrite(ctx, client, path, f, transcoder)
	} else {
		size, err = bucket.Write(ctx, client, path, f)
	}
	return size, err
}

// writeThumbnailToBucket uploads thumbnail to bucket.
func writeThumbnailToBucket(ctx context.Context, client *storage.Client, path string, file *multipart.FileHeader) (int64, error) {
	thumbnailPath := toThumbnailPath(path)
	return writeFileToBucket(ctx, client, thumbnailPath, file, thumbnailTranscoder)
}

// thumbnailTranscoder the transcoder to transcode image to thumbnail.
func thumbnailTranscoder(thumbnailWriter io.Writer, imageReader io.Reader) (int64, error) {
	img, err := imaging.Decode(imageReader)
	if err != nil {
		log.Printf("File decoded failed: %s", err)
		return 0, err
	}

	thumbnail := imaging.Thumbnail(img, thumbnailWidth, thumbnailHeight, imaging.CatmullRom)
	dst := imaging.New(thumbnailWidth, thumbnailHeight, color.NRGBA{0, 0, 0, 0})
	dst = imaging.Paste(dst, thumbnail, image.Pt(0, 0))

	if err = imaging.Encode(thumbnailWriter, dst, imaging.PNG); err != nil {
		return 0, nil
	}
	return -1, err // Unknow written size.
}

// uploadToBucket uploads file with thumbnail to bucket.
func uploadToBucket(ctx context.Context, client *storage.Client, path string, file *multipart.FileHeader) (int64, error) {
	size, err := writeFileToBucket(ctx, client, path, file, nil)
	if err != nil {
		return -1, err
	}

	// Upload thumbnail if it's an image.
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if slices.Contains(imageTypes, ext) {
		if _, err := writeThumbnailToBucket(ctx, client, path, file); err != nil {
			return -1, err
		}
	}
	return size, nil
}

// updateBucketFile deletes old file with thumbnail and upload new one to bucket.
func updateBucketFile(ctx context.Context, path string, file *multipart.FileHeader) (id string, newPath string, size int64, err error) {
	defer func() {
		if err != nil {
			log.Printf("Update bucket path %s failed: %s", path, err)
		}
	}()

	client := bucket.NewClient(ctx)
	defer client.Close() // nolint: errcheck

	if err = deleteBucketFile(ctx, client, path); err != nil {
		return
	}
	id = uuid.New().String()
	newPath = toBucketPath(id)
	size, err = uploadToBucket(ctx, client, newPath, file)
	return
}

// deleteBucketFile deletes file with thumbnail from cloud storage bucket.
func deleteBucketFile(ctx context.Context, client *storage.Client, path string) error {
	if path == "" {
		log.Println("no path to delete")
		return nil
	}
	thumbnailPath := toThumbnailPath(path)
	// The path order is matter, delete file before thumbnail.
	if _, err := bucket.Delete(ctx, client, path, thumbnailPath); err != nil {
		return err
	}
	return nil
}

// generateFileMeta gets data from <doc> then return a FileMeta instance.
func generateFileMeta(result *firestore.FileMeta) FileMeta {
	var meta FileMeta
	log.Println("result:", result.ID, result.Name, result.Path, result.Tags, result.OrderNo)

	meta.ID = result.ID
	meta.Name = result.Name
	meta.Tags = result.Tags
	meta.OrderNo = result.OrderNo
	meta.FileSize = result.FileSize
	meta.CreateTime = result.CreateTime.Format("2006-01-02T15:04:05.000Z")
	meta.UpdateTime = result.UpdateTime.Format("2006-01-02T15:04:05.000Z")
	meta.URL = toResourceURL(result.Path)
	ext := strings.ToLower(filepath.Ext(meta.Name))
	if slices.Contains(imageTypes, ext) {
		meta.ThumbURL = toResourceURL(toThumbnailPath(result.Path))
	} else {
		meta.ThumbURL = ""
	}
	log.Println("final meta:", meta)
	return meta
}

// response composes the http response.
func response(c *gin.Context, code int, body interface{}) {
	if body == nil {
		c.String(code, "")
	} else {
		c.JSON(code, body)
	}
}

// PostFiles is function for /api/files POST endpoint.
// This API uses `multipart/form-data` to upload multiple files along with the relevant tags in a single request.
func PostFiles(c *gin.Context) {
	obj := &FileUploadRequest{}
	if err := c.Bind(obj); err != nil {
		response(c, http.StatusBadRequest, nil)
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		response(c, http.StatusBadRequest, nil)
		return
	}

	files := form.File["files"]
	tags := parseTags(form.Value["tags"][0])

	ctx := context.Background()
	client := bucket.NewClient(ctx)
	defer client.Close() // nolint: errcheck

	dbClient := firestore.NewClient(ctx)
	defer dbClient.Close() // nolint: errcheck

	var filesarray []FileMeta
	// Iterate all uploaded files.
	for _, file := range files {
		filename := filepath.Base(file.Filename)
		log.Println("Process uploaded file:", filename)

		id := uuid.New().String()
		path := toBucketPath(id)
		size, err := uploadToBucket(ctx, client, path, file)
		if err != nil {
			response(c, http.StatusBadRequest, nil)
			return
		}

		// Add data to firestore.
		record := &firestore.FileMetaRec{
			Path:     path,
			Name:     filename,
			FileSize: size,
			Tags:     tags,
			OrderNo:  getOrderNo(id),
		}
		docSnap, err := firestore.Create(ctx, dbClient, id, record)
		if err != nil {
			log.Panicln(err)
		}

		// Add data to response.
		item := generateFileMeta(docSnap)
		filesarray = append(filesarray, item)
		log.Printf("Uploaded file: %v\n", filename)
	}
	response(c, http.StatusCreated, &FileListResponse{Files: filesarray})
}

// UpdateFile is function for /api/files/{id} UPDATE endpoint.
// This API enables users to modify the file identified by the ID.
func UpdateFile(c *gin.Context) {
	ctx := context.Background()
	id := c.Param("id")

	var err error
	dbClient := firestore.NewClient(ctx)
	defer dbClient.Close() // nolint: errcheck

	// Make suer the file exists before updating.
	meta, err := firestore.Get(ctx, dbClient, id)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			response(c, http.StatusNotFound, nil)
			return
		} else {
			log.Panicln(err)
		}
	}

	form, _ := c.MultipartForm()
	var file *multipart.FileHeader
	tags := parseTags(form.Value["tags"][0])
	log.Println("tags:", tags)

	if len(form.File["file"]) != 0 {
		file = form.File["file"][0]
	} else {
		file = nil
	}

	fields := map[string]interface{}{
		firestore.FieldTags:    tags,
		firestore.FieldOrderNo: getOrderNo(id),
	}
	if file != nil {
		log.Println("file:", file.Filename)
		bucketFileID, newPath, size, err := updateBucketFile(ctx, meta.Path, file)
		log.Println("bucketID:", bucketFileID, ", newPath:", newPath, ", err:", err)
		if err != nil {
			log.Panicln(err)
		}
		fields[firestore.FieldPath] = newPath
		fields[firestore.FieldName] = filepath.Base(file.Filename)
		fields[firestore.FieldSize] = size
	}
	newMeta, err := firestore.Merge(ctx, dbClient, id, &fields)
	if err != nil {
		log.Panicln(err)
	}

	item := generateFileMeta(newMeta)
	response(c, http.StatusOK, &FileUpdateResponse{File: item})
}

// GetFileList is function for /api/files GET endpoint.
// This API offers optional query parameters `tags` and `orderNo` to filter files.
// The files are listed in order of `orderNo` based on last update time with a default page size of 50.
func GetFileList(c *gin.Context) {
	tags := parseTags(c.Query("tags"))
	orderNo := c.Query("orderNo")
	size, err := parsePageSize(c.Query("size"))
	if err != nil {
		response(c, http.StatusBadRequest, nil)
		return
	}

	ctx := context.Background()
	dbClient := firestore.NewClient(ctx)
	defer dbClient.Close() // nolint: errcheck

	docs, err := firestore.ListByTags(ctx, dbClient, tags, orderNo, size)
	if err != nil {
		log.Panicln(err)
	}

	results := []FileMeta{}
	for _, doc := range docs {
		item := generateFileMeta(doc)
		results = append(results, item)
	}

	response(c, http.StatusOK, &FileListResponse{Files: results})
}

// DeleteFile is function for /api/files/{id} DELETE endpoint.
// This API enables users to delete the file identified by the ID.
func DeleteFile(c *gin.Context) {
	ctx := context.Background()
	id := c.Param("id")

	var err error
	dbClient := firestore.NewClient(ctx)
	defer dbClient.Close() // nolint: errcheck

	// Delete data in firestore.
	doc, err := firestore.Get(ctx, dbClient, id)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			response(c, http.StatusNotFound, nil)
			return
		} else {
			log.Panicln(err)
		}
	}

	client := bucket.NewClient(ctx)
	defer client.Close() // nolint: errcheck

	if err := deleteBucketFile(ctx, client, doc.Path); err != nil {
		log.Panicln(err)
	}
	if err := firestore.Delete(ctx, dbClient, id); err != nil {
		log.Panicln(err)
	}

	log.Printf("Object %q deleted.\n", id)
	response(c, http.StatusNoContent, nil)
}
