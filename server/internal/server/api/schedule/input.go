package schedule

import schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"

// ScheduleSettingsInput is shared by direct creation, approval, and updates.
// A nil ForceH264 retains the stored value on update and defaults on creation.
type ScheduleSettingsInput struct {
	RecordingType  string `json:"recording_type,omitempty" validate:"omitempty,oneof=video audio"`
	Quality        string `json:"quality" validate:"required,oneof=LOW MEDIUM HIGH 1440 BEST"`
	ForceH264      *bool  `json:"force_h264,omitempty"`
	HasMinViewers  bool   `json:"has_min_viewers"`
	MinViewers     *int64 `json:"min_viewers,omitempty" validate:"omitempty,min=0"`
	HasCategories  bool   `json:"has_categories"`
	HasTags        bool   `json:"has_tags"`
	IsDeleteRediff bool   `json:"is_delete_rediff"`
	// TimeBeforeDelete is validated only when IsDeleteRediff is set; an
	// unconditional bound would reject inactive retention settings.
	TimeBeforeDelete *int64   `json:"time_before_delete,omitempty"`
	IsDisabled       bool     `json:"is_disabled"`
	CategoryIDs      []string `json:"category_ids"`
	TagIDs           []int64  `json:"tag_ids"`
}

func (input ScheduleSettingsInput) writeInput(broadcasterID string) schedulesvc.WriteInput {
	return schedulesvc.WriteInput{
		BroadcasterID:    broadcasterID,
		RecordingType:    input.RecordingType,
		Quality:          input.Quality,
		ForceH264:        input.ForceH264,
		HasMinViewers:    input.HasMinViewers,
		MinViewers:       input.MinViewers,
		HasCategories:    input.HasCategories,
		HasTags:          input.HasTags,
		IsDeleteRediff:   input.IsDeleteRediff,
		TimeBeforeDelete: input.TimeBeforeDelete,
		IsDisabled:       input.IsDisabled,
		CategoryIDs:      input.CategoryIDs,
		TagIDs:           input.TagIDs,
	}
}
