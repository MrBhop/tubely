package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func (cfg apiConfig) getObjectURL(key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
}
func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port,  assetPath)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func getAssetPath(mediaType string) (string, error) {
	id := make([]byte, 32)
	_, err := rand.Read(id)
	if err != nil {
		return "", err
	}
	extension := mediaTypeToExtension(mediaType)
	return fmt.Sprintf("%s%s", base64.RawURLEncoding.EncodeToString(id), extension), nil
}

func mediaTypeToExtension(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}

	return "." + parts[1]
}

func getVideoAspectRatio(filePath string) (string, error) {
	type ffprobeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	commandOutput := bytes.Buffer{}
	command.Stdout = &commandOutput
	err := command.Run()
	if err != nil {
		return "", err
	}

	var videoInformation ffprobeOutput
	decoder := json.NewDecoder(&commandOutput)
	if err := decoder.Decode(&videoInformation); err != nil {
		return "", err
	}
	
	width := videoInformation.Streams[0].Width
	height := videoInformation.Streams[0].Height
	trueAspectRatio := float64(width) / float64(height)

	const (
		aspectRatio169 float64 = float64(16) / float64(9)
		aspectRatio916 float64 = float64(9) / float64(16)
	)

	if isAspectRatio(trueAspectRatio, aspectRatio169) {
		return "landscape", nil
	}
	if isAspectRatio(trueAspectRatio, aspectRatio916) {
		return "portrait", nil
	}
	return "other", nil
}

func isAspectRatio(value, checkAgainst float64) bool {
	const tolerance = 0.01

	if math.Abs(value - checkAgainst) < tolerance {
		return true
	}
	return false
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	signedClient := s3.NewPresignClient(s3Client)
	signedRequest, err := signedClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Key: &key,
		Bucket: &bucket,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return signedRequest.URL, nil
}
