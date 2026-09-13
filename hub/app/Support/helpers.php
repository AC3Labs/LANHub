<?php

if (! function_exists('human_filesize')) {
    function human_filesize(int $bytes): string
    {
        $units = ['B', 'KB', 'MB', 'GB', 'TB'];
        $bytes = max($bytes, 0);
        $power = $bytes > 0 ? (int) floor(log($bytes, 1024)) : 0;
        $power = min($power, count($units) - 1);

        return round($bytes / (1024 ** $power), $power === 0 ? 0 : 1).' '.$units[$power];
    }
}

if (! function_exists('is_previewable_image')) {
    /**
     * Matches the image types agent/agent.py's GET /api/preview will
     * actually stream — used by the Explorer grid to decide whether to
     * render a real thumbnail instead of the generic file-icon.
     */
    function is_previewable_image(string $filename): bool
    {
        $extension = strtolower(pathinfo($filename, PATHINFO_EXTENSION));

        return in_array($extension, ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg'], true);
    }
}
