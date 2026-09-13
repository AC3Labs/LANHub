@props(['active'])

@php
$classes = ($active ?? false)
            ? 'inline-flex items-center px-3 py-1.5 rounded-md text-sm font-medium leading-5 text-tan-800 bg-tan-100 focus:outline-none transition duration-150 ease-in-out'
            : 'inline-flex items-center px-3 py-1.5 rounded-md text-sm font-medium leading-5 text-stone-500 hover:text-stone-800 hover:bg-stone-100 focus:outline-none transition duration-150 ease-in-out';
@endphp

<a {{ $attributes->merge(['class' => $classes]) }}>
    {{ $slot }}
</a>
