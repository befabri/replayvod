import { createReadStream, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, resolve, sep } from "node:path";

const root = resolve("dist/client");
if (!existsSync(resolve(root, "index.html")))
	throw new Error(
		"Build the dashboard before serving it to the browser suite.",
	);
const mime = {
	".html": "text/html",
	".js": "text/javascript",
	".css": "text/css",
	".json": "application/json",
	".svg": "image/svg+xml",
	".png": "image/png",
	".ico": "image/x-icon",
	".woff2": "font/woff2",
	".woff": "font/woff",
};
// Local test server only. History fallback matches the Go server's SPA routes.
createServer((req, res) => {
	let path;
	try {
		path = resolve(
			root,
			`.${decodeURIComponent(new URL(req.url, "http://localhost").pathname)}`,
		);
	} catch {
		res.writeHead(400).end();
		return;
	}
	if (path !== root && !path.startsWith(root + sep)) {
		res.writeHead(403).end();
		return;
	}
	if (path === root) path = resolve(root, "index.html");
	if (!existsSync(path) || !statSync(path).isFile()) {
		if (!(req.headers.accept ?? "").includes("text/html")) {
			res.writeHead(404).end();
			return;
		}
		path = resolve(root, "index.html");
	}
	res.writeHead(200, {
		"Content-Type": mime[extname(path)] ?? "application/octet-stream",
		"Cache-Control": "no-store",
	});
	if (req.method === "HEAD") {
		res.end();
		return;
	}
	createReadStream(path)
		.on("error", () => res.destroy())
		.pipe(res);
}).listen(39174, "127.0.0.1");
