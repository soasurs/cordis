package server

import (
	"context"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

func (s *MediaServer) GetImageUploadConstraints(
	_ context.Context,
	req *mediav1.GetImageUploadConstraintsRequest,
) (*mediav1.GetImageUploadConstraintsResponse, error) {
	purpose, err := imagePurposeFromRequest(req)
	if err != nil {
		return nil, err
	}
	profile := s.svcCtx.Cfg.Media.ImageConstraintsFor(purpose)
	constraints := new(mediav1.ImageUploadConstraints)
	constraints.SetMaxFileSizeBytes(profile.MaxSizeBytes)
	constraints.SetMaxWidth(profile.MaxDimension)
	constraints.SetMaxHeight(profile.MaxDimension)
	constraints.SetMaxPixels(profile.MaxPixels)
	constraints.SetAllowedContentTypes(append([]string(nil), profile.AllowedContentTypes...))
	resp := new(mediav1.GetImageUploadConstraintsResponse)
	resp.SetConstraints(constraints)
	return resp, nil
}

func imagePurposeFromRequest(req *mediav1.GetImageUploadConstraintsRequest) (config.ImagePurpose, error) {
	switch {
	case req.HasUserAvatar():
		return config.ImagePurposeUserAvatar, nil
	case req.HasGuildIcon():
		return config.ImagePurposeGuildIcon, nil
	default:
		return "", errPurposeRequired
	}
}

func imageConstraintsForKind(cfg config.MediaConfig, kind store.Kind) config.ImageConstraintProfile {
	switch kind {
	case store.KindUserAvatar:
		return cfg.ImageConstraintsFor(config.ImagePurposeUserAvatar)
	case store.KindGuildIcon:
		return cfg.ImageConstraintsFor(config.ImagePurposeGuildIcon)
	default:
		return cfg.ImageConstraintsFor("")
	}
}
