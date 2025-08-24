package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30

	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading video", videoID, "by user", userID)

	videoData, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "ID does not match any videos", err)
	}
	if videoData.UserID.ID() != userID.ID() {
		respondWithError(w, http.StatusUnauthorized, "User is not the owner of the video", nil)
		return
	}

	videoFile, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse video form file", err)
		return
	}
	defer videoFile.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type, only mp4 allowed", nil)
		return
	}

	tempVideoFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file for video", err)
		return
	}
	defer os.Remove(tempVideoFile.Name())
	defer tempVideoFile.Close()

	if _, err := io.Copy(tempVideoFile, videoFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying video to disk", err)
		return
	}

	if _, err := tempVideoFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting file offset", err)
		return
	}

	processedVideoPath, err := processVideoForFastStart(tempVideoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing video file", err)
		return
	}
	defer os.Remove(processedVideoPath)
	processedVideoFile, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening processed video file", err)
		return
	}
	defer processedVideoFile.Close()

	aspectRatio, err := getVideoAspectRatio(processedVideoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting aspect ratio from video", err)
		return
	}
	key, err := getAssetPath(mediaType)
	key = fmt.Sprintf("%s/%s", aspectRatio, key)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to generate asset path", err)
		return
	}
	if _, err := cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &key,
		Body: processedVideoFile,
		ContentType: &mediaType,
	}); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error sending video to s3 bucket", err)
		return
	}
	
	newVideoUrl := fmt.Sprintf("%s,%s", cfg.s3Bucket, key)
	videoData.VideoURL = &newVideoUrl
	if err := cfg.db.UpdateVideo(videoData); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video information", err)
		return
	}

	cfg.respondWithVideo(w, http.StatusOK, videoData)
}

func (cfg *apiConfig) respondWithVideo(w http.ResponseWriter, code int, video database.Video) {
	presignedVideo, err := cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error, generating presigned video url", err)
		return
	}

	respondWithJSON(w, code, presignedVideo)
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"
	command := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	if err := command.Run(); err != nil {
		return "", err
	}

	return outputPath, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	// if the video url is nil, there's nothing to sign.
	if video.VideoURL == nil {
		return video, nil
	}

	parts := strings.Split(*video.VideoURL, ",")
	if length := len(parts); length != 2 {
		return video, fmt.Errorf("Expected two comma delimited parts, got %d", length)
	}

	presignedUrl, err := generatePresignedURL(cfg.s3Client, parts[0], parts[1], 10 * time.Minute)
	if err != nil {
		return video, fmt.Errorf("Error generating presigned url: %v", err)
	}
	video.VideoURL = &presignedUrl

	return video, nil
}
