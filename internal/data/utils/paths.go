package utils

// ExcludedDirs is a set of directory names to exclude from traversal
var ExcludedDirs = map[string]bool{
	".stryker-tmp":  true,
	"node_modules":  true,
	"dist":          true,
	"build":         true,
	"__pycache__":   true,
	".pytest_cache": true,
	"target":        true,
	".git":          true,
	"coverage":      true,
	".nyc_output":   true,
	".next":         true,
	".nuxt":         true,
	"vendor":        true,
	".venv":         true,
	"venv":          true,
	"env":           true,
	".env":          true,
	".idea":         true,
	".vscode":       true,
	".gradle":       true,
	".mvn":          true,
	"bin":           true,
	"obj":           true,
	"out":           true,
	".cache":        true,
	"tmp":           true,
	"temp":          true,
	".terraform":    true,
	".serverless":   true,
	".aws-sam":      true,
}

// IsExcludedDir checks if a directory name should be excluded
func IsExcludedDir(name string) bool {
	return ExcludedDirs[name]
}
