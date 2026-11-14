package db

import (
	commonv1 "github.com/schemo-app/proto/go/common/v1"
	historyv1 "github.com/schemo-app/proto/go/history/v1"

	"schemo/internal/domain"
)

// RecordSource converts an application value to the wire enum.
func RecordSource(value string) historyv1.RecordSource {
	switch value {
	case "manual":
		return historyv1.RecordSource_RECORD_SOURCE_MANUAL
	case "browser":
		return historyv1.RecordSource_RECORD_SOURCE_BROWSER
	default:
		return historyv1.RecordSource_RECORD_SOURCE_UNSPECIFIED
	}
}

// RecordSourceIn converts a known wire enum to an application value.
func RecordSourceIn(value historyv1.RecordSource) string {
	switch value {
	case historyv1.RecordSource_RECORD_SOURCE_MANUAL:
		return "manual"
	case historyv1.RecordSource_RECORD_SOURCE_BROWSER:
		return "browser"
	default:
		return ""
	}
}

// BodySide converts an application value to the wire enum.
func BodySide(value string) historyv1.BodySide {
	switch value {
	case "request":
		return historyv1.BodySide_BODY_SIDE_REQUEST
	case "response":
		return historyv1.BodySide_BODY_SIDE_RESPONSE
	default:
		return historyv1.BodySide_BODY_SIDE_UNSPECIFIED
	}
}

// BodySideIn converts a known wire enum to an application value.
func BodySideIn(value historyv1.BodySide) string {
	switch value {
	case historyv1.BodySide_BODY_SIDE_REQUEST:
		return "request"
	case historyv1.BodySide_BODY_SIDE_RESPONSE:
		return "response"
	default:
		return ""
	}
}

var features = map[commonv1.Feature]domain.Feature{
	commonv1.Feature_FEATURE_SYNC:              domain.FeatureSync,
	commonv1.Feature_FEATURE_SHARED_WORKSPACES: domain.FeatureSharedSpaces,
	commonv1.Feature_FEATURE_ROLES:             domain.FeatureRoles,
	commonv1.Feature_FEATURE_HISTORY:           domain.FeatureHistory,
	commonv1.Feature_FEATURE_AUDIT:             domain.FeatureAudit,
	commonv1.Feature_FEATURE_SHARE_LINKS:       domain.FeatureShareLinks,
	commonv1.Feature_FEATURE_SECRETS:           domain.FeatureSecrets,
	commonv1.Feature_FEATURE_BILLING:           domain.FeatureBilling,
}

// Features reads the features this client recognizes.
func Features(values []commonv1.Feature) []domain.Feature {
	out := make([]domain.Feature, 0, len(values))
	for _, value := range values {
		if feature, ok := features[value]; ok {
			out = append(out, feature)
		}
	}
	return out
}
