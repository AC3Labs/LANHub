@props(['disabled' => false])

<input @disabled($disabled) {{ $attributes->merge(['class' => 'border-stone-300 text-stone-800 focus:border-tan-400 focus:ring-tan-400 rounded-md shadow-sm']) }}>
