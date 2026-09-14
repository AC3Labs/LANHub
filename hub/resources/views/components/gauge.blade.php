@props(['label', 'valueLabel', 'percent' => 0, 'color' => '#ac8544'])

@php
    // Semicircular arc gauge, drawn with a single round-cap stroke and
    // filled via stroke-dasharray — the standard SVG dial trick, no
    // charting library needed for one arc per card.
    $radius = 42;
    $circumference = M_PI * $radius;
    $clamped = max(0, min(100, $percent));
    $offset = $circumference - ($clamped / 100) * $circumference;
@endphp

<div {{ $attributes->merge(['class' => 'flex flex-1 flex-col items-center text-center']) }}>
    <svg viewBox="0 0 100 58" class="w-full max-w-[11rem]">
        <path d="M 8 50 A {{ $radius }} {{ $radius }} 0 0 1 92 50"
              fill="none" stroke="currentColor" stroke-width="8" stroke-linecap="round"
              class="text-stone-100" />
        <path d="M 8 50 A {{ $radius }} {{ $radius }} 0 0 1 92 50"
              fill="none" stroke="{{ $color }}" stroke-width="8" stroke-linecap="round"
              stroke-dasharray="{{ $circumference }}" stroke-dashoffset="{{ $offset }}"
              class="transition-[stroke-dashoffset] duration-700 ease-out" />
    </svg>
    <p class="-mt-3 text-xs font-semibold uppercase tracking-wide text-stone-400">{{ $label }}</p>
    <p class="text-xl font-bold text-stone-800 tabular-nums mt-0.5">{{ $valueLabel }}</p>
</div>
