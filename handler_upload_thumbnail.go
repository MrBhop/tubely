package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 20

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


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	r.ParseMultipartForm(maxMemory)

	imageFile, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer imageFile.Close()

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusInternalServerError, "Missing Content-Type for thumbnail", nil)
		return
	}

	switch mediaType {
	case "image/jpeg":
		fallthrough
	case "image/png":
		// do nothing - this is valid.
		break
	default:
		respondWithError(w, http.StatusBadRequest, "Invalid File Type. Expected image/jpeg or image/png", nil)
		return
	}

	videoData, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "ID does not match any videos", nil)
		return
	}
	if videoData.UserID.ID() != userID.ID() {
		respondWithError(w, http.StatusUnauthorized, "User is not the owner of the video", nil)
		return
	}

	assetPath, err := getAssetPath(mediaType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to generate asset path", err)
		return
	}
	imageSavePath := cfg.getAssetDiskPath(assetPath)
	saveFile, err := os.Create(imageSavePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating image file on disk", err)
		return
	}
	defer saveFile.Close()

	if _, err := io.Copy(saveFile, imageFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying image to disk", err)
		return
	}

	thumbnailUrl := cfg.getAssetURL(assetPath)
	videoData.ThumbnailURL = &thumbnailUrl
	if err := cfg.db.UpdateVideo(videoData); err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error updating video information", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoData)
}
