import type { QueryClient } from "@tanstack/react-query";
import {
	createRootRouteWithContext,
	HeadContent,
	Scripts,
} from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import appCss from "@/styles.css?url";
import "@/i18n";
import { Toaster } from "@/components/ui/toaster";
import { useAuthInit } from "@/hooks/useAuthInit";

interface MyRouterContext {
	queryClient: QueryClient;
}

// Dark is the :root default; .light class opts into the light palette.
// Also migrates the legacy v1 `darkMode` boolean key to the new `theme` key.
const THEME_INIT_SCRIPT = `(function(){try{var ls=window.localStorage;var stored=ls.getItem('theme');if(!stored){var legacy=ls.getItem('darkMode');if(legacy==='true'){stored='dark';ls.setItem('theme','dark')}else if(legacy==='false'){stored='light';ls.setItem('theme','light')}if(legacy!==null)ls.removeItem('darkMode')}var mode=(stored==='light'||stored==='dark')?stored:'dark';var root=document.documentElement;root.classList.remove('light');if(mode==='light')root.classList.add('light');root.setAttribute('data-theme',mode);}catch(e){}})();`;

export const Route = createRootRouteWithContext<MyRouterContext>()({
	head: () => ({
		meta: [
			{ charSet: "utf-8" },
			{ name: "viewport", content: "width=device-width, initial-scale=1" },
			{ title: "ReplayVOD" },
		],
		links: [
			{
				rel: "stylesheet",
				href: appCss,
			},
		],
	}),
	shellComponent: RootDocument,
});

function RootDocument({ children }: { children: React.ReactNode }) {
	// Hydrate auth store from session cookie on app startup
	useAuthInit();
	const { i18n } = useTranslation();

	// Keep <html lang> in sync with i18n so screen readers, browser
	// translation prompts, and :lang() CSS selectors follow the UI
	// language. SPA mode — no SSR lang to worry about.
	useEffect(() => {
		const base = i18n.language.split("-")[0] || "en";
		if (document.documentElement.lang !== base) {
			document.documentElement.lang = base;
		}
	}, [i18n.language]);

	return (
		<html lang="en" suppressHydrationWarning>
			<head>
				<script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
				<HeadContent />
			</head>
			<body className="font-sans antialiased">
				{children}
				<Toaster />
				<Scripts />
			</body>
		</html>
	);
}
