// This is a minimal example parser plugin that demonstrates the required behavior.
// It simulates handling a YouTube video link.

/**
 * Plugin metadata.
 * The version is the plugin spec version supported by Teleload and is required.
 */
const metadata = {
    name: "Example Parser", // Plugin name
    version: "1.0.0", // Plugin version
    description: "A parser for example links", // Plugin description
    author: "Krau", // Plugin author
}

// You can use console.log to emit logs through the Go logger.
console.log("Parser loaded", "name", metadata.name);

/**
 * canHandle determines whether this parser can handle the given URL.
 */
const canHandle = function (url) {
    // Here we simply check if the URL contains "youtube.com/watch?v".
    return url.includes("youtube.com/watch?v");
}

/**
 * Parse the URL and return an Item object defined in pkg/parser.go.
 */
const parse = function (url) {
    var result = {
        // Metadata
        site: "YouTube",
        url: url,
        title: "Sample YouTube Video",
        author: "Example Creator",
        description: "This is a sample video entry.",
        tags: ["test", "youtube"],
        // Resources (downloadable files)
        resources: [
            {
                url: "https://example.com/video1.mp4", // Direct file URL
                filename: "somevideo.mp4", // File name
                mime_type: "video/mp4", // MIME type (optional)
                extension: "mp4", // File extension (optional)
                size: 100 * 1024 * 1024, // File size in bytes, use 0 if unknown
                hash: {}, // File hashes (optional), e.g. {"md5": "xxx", "sha256": "xxx"}
                headers: {}, // HTTP headers required to download the file (optional)
                extra: {} // Additional data (optional)
            },
            {
                url: "https://example.com/picture1.png",
                filename: "picture1.png",
                mime_type: "image/png",
                extension: "png",
                size: 1 * 1024 * 1024,
                hash: {},
                headers: {},
                extra: {}
            }
        ],
        extra: {}
    };
    return result;
}

// Finally, call registerParser to register this parser.
registerParser({
    metadata,
    canHandle,
    parse
});

// For more advanced plugin authoring, see plugins/example_parser_danbooru.js.
