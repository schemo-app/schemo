package db

import (
	commonv1 "github.com/schemo-app/proto/go/common/v1"
	historyv1 "github.com/schemo-app/proto/go/history/v1"
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

// Features reads the features this client recognizes.
func Features(values []commonv1.Feature) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch value {
		case commonv1.Feature_FEATURE_SYNC:
			out = append(out, "sync")
		case commonv1.Feature_FEATURE_SHARED_WORKSPACES:
			out = append(out, "shared_spaces")
		case commonv1.Feature_FEATURE_ROLES:
			out = append(out, "roles")
		case commonv1.Feature_FEATURE_HISTORY:
			out = append(out, "history")
		case commonv1.Feature_FEATURE_AUDIT:
			out = append(out, "audit")
		case commonv1.Feature_FEATURE_SHARE_LINKS:
			out = append(out, "share_links")
		case commonv1.Feature_FEATURE_SECRETS:
			out = append(out, "secrets")
		case commonv1.Feature_FEATURE_BILLING:
			out = append(out, "billing")
		}
	}
	return out
}
