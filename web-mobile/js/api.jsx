// Backward-compatibility shim. The original api.jsx contained both the
// HTTP client (the API object, fetch calls, SSE subscription) and the
// pure serializer/normalizer helpers. Per NFR-006 these were split
// (T006/T007) into:
//   - api_client.jsx     — the API object and SSE subscription
//   - api_serializers.jsx — pure helpers (normalizeMatch, etc.)
//
// This file remains as a re-export shim so existing importers
// (`import { ... } from './api.jsx'` in tests; window.API in browser
// runtime) keep working without churn. New code SHOULD import directly
// from api_client.jsx or api_serializers.jsx.
//
// T010 decision: shim chosen over delete-and-update-importers because
// __tests__/api.test.jsx imports directly from this path; the shim
// preserves the public surface with zero test churn while still
// physically separating fetch from serialization.

import {
    toBackendStatus,
    isHikiwake,
    toBackendMatchResult,
    normalizeMatch,
    buildPlayerMap,
    normalizePlayer,
    normalizeCompetitionDetail,
} from './api_serializers.jsx';
import { API } from './api_client.jsx';

export {
    toBackendStatus,
    isHikiwake,
    toBackendMatchResult,
    normalizeMatch,
    buildPlayerMap,
    normalizePlayer,
    normalizeCompetitionDetail,
    API,
};
