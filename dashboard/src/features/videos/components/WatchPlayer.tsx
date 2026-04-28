import "@vidstack/react/player/styles/default/theme.css";
import "@vidstack/react/player/styles/default/layouts/video.css";
import "./WatchPlayer.css";

import { MediaPlayer, MediaProvider } from "@vidstack/react";
import {
	DefaultVideoLayout,
	defaultLayoutIcons,
} from "@vidstack/react/player/layouts/default";
import { API_URL } from "@/env";

// WatchPlayer wraps Vidstack's MediaPlayer with the app's defaults so
// the route only has to pass src + title. The native HTML <video>
// element is still rendered under the hood by MediaProvider — we're
// just trading the browser's built-in chrome for Vidstack's default
// layout, which gives us a styleable scrubber, time display, and the
// usual playback/volume/fullscreen/PiP controls.
//
// `src` is passed as an object with explicit `type` so Vidstack can
// pick its loader without a HEAD probe — the streaming endpoint
// serves a single MP4 per video, no HLS/DASH/etc.
//
// `crossOrigin` is only set when the API runs on a different origin
// (VITE_API_URL non-empty). Setting `use-credentials` on a same-
// origin URL forces CORS mode and the request fails unless the
// server returns Access-Control-Allow-Credentials, which it
// doesn't bother to do for same-origin in dev. When VITE_API_URL is
// configured we're cross-origin and the streaming middleware does
// emit the credential-allowing CORS headers.
export function WatchPlayer({ src, title }: { src: string; title: string }) {
	const isCrossOrigin = !!API_URL;
	return (
		<MediaPlayer
			src={{ src, type: "video/mp4" }}
			title={title}
			crossOrigin={isCrossOrigin ? "use-credentials" : null}
			playsInline
			className="rounded-lg overflow-hidden bg-black shadow-sm"
		>
			<MediaProvider />
			<DefaultVideoLayout icons={defaultLayoutIcons} />
		</MediaPlayer>
	);
}
