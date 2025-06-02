package main

var colors = []string{
	"\033[31m", // Red
	"\033[32m", // Green
	"\033[33m", // Yellow
	"\033[34m", // Blue
	"\033[35m", // Magenta
	"\033[36m", // Cyan
	"\033[91m", // Bright Red
	"\033[92m", // Bright Green
	"\033[93m", // Bright Yellow
	"\033[94m", // Bright Blue
	"\033[95m", // Bright Magenta
	"\033[96m", // Bright Cyan
}

const resetColor = "\033[0m"

func assignColors(directories []string) map[string]string {
	colorMap := make(map[string]string)
	for i, dir := range directories {
		colorMap[dir] = colors[i%len(colors)]
	}
	return colorMap
}
