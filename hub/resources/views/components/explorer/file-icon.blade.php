@props(['type', 'name', 'size' => 'sm'])

@php
    $ext = strtolower(pathinfo($name ?? '', PATHINFO_EXTENSION));

    $group = match (true) {
        in_array($type, ['dir', 'drive']) => 'folder',
        in_array($ext, ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp']) => 'image',
        in_array($ext, ['mp4', 'mov', 'mkv', 'avi', 'webm']) => 'video',
        in_array($ext, ['mp3', 'wav', 'flac', 'ogg']) => 'audio',
        in_array($ext, ['zip', 'rar', '7z', 'tar', 'gz']) => 'archive',
        in_array($ext, ['pdf']) => 'pdf',
        in_array($ext, ['php', 'js', 'ts', 'py', 'rb', 'go', 'rs', 'java', 'c', 'cpp', 'json', 'yml', 'yaml', 'sh']) => 'code',
        in_array($ext, ['txt', 'md', 'doc', 'docx']) => 'document',
        default => 'file',
    };

    $colors = [
        'folder' => 'text-tan-500',
        'image' => 'text-emerald-500',
        'video' => 'text-purple-500',
        'audio' => 'text-pink-500',
        'archive' => 'text-amber-600',
        'pdf' => 'text-red-500',
        'code' => 'text-sky-600',
        'document' => 'text-stone-500',
        'file' => 'text-stone-400',
    ];

    $dimension = $size === 'lg' ? 'w-9 h-9' : 'w-4 h-4';
@endphp

<svg class="{{ $dimension }} {{ $colors[$group] }} shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    @if ($group === 'folder')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z"/>
    @elseif ($group === 'image')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14M4 8h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"/>
    @elseif ($group === 'video')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"/>
    @elseif ($group === 'audio')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M9 19V6l10-2v13M9 19a2 2 0 11-4 0 2 2 0 014 0zm10-2a2 2 0 11-4 0 2 2 0 014 0z"/>
    @elseif ($group === 'archive')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>
    @elseif ($group === 'pdf' || $group === 'document')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
    @elseif ($group === 'code')
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M10 20l4-16m4 4l4 4-4 4M6 8l-4 4 4 4"/>
    @else
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/>
    @endif
</svg>
