@props(['os', 'class' => 'w-4 h-4'])

@switch($os)
    @case('windows')
        {{-- The current (2012+) four-pane Windows logo, in its real brand colors. --}}
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="{{ $class }}">
            <rect x="2" y="2" width="9.3" height="9.3" fill="#F25022"/>
            <rect x="12.7" y="2" width="9.3" height="9.3" fill="#7FBA00"/>
            <rect x="2" y="12.7" width="9.3" height="9.3" fill="#00A4EF"/>
            <rect x="12.7" y="12.7" width="9.3" height="9.3" fill="#FFB900"/>
        </svg>
        @break
    @case('macos')
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="{{ $class }}">
            <path d="M17.05 12.5c-.03-2.77 2.26-4.1 2.36-4.16-1.29-1.89-3.3-2.15-4.02-2.18-1.71-.17-3.34 1.01-4.21 1.01-.87 0-2.2-.98-3.62-.96-1.86.03-3.58 1.08-4.54 2.75-1.94 3.36-.5 8.33 1.39 11.06.93 1.34 2.03 2.83 3.48 2.78 1.4-.06 1.93-.9 3.62-.9s2.17.9 3.65.87c1.51-.03 2.46-1.36 3.38-2.71.81-1.16 1.16-1.8 1.88-3.15-4.94-1.87-4.34-6.78-1.37-7.41zM14.4 4.6c.77-.93 1.29-2.23 1.15-3.6-1.12.05-2.5.75-3.31 1.68-.72.83-1.35 2.17-1.18 3.44 1.29.1 2.6-.65 3.34-1.52z"/>
        </svg>
        @break
    @case('linux')
        {{-- Tux, the actual Linux mascot/logo, simplified to a flat vector. --}}
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="{{ $class }}">
            <path d="M12 2c-2.2 0-3.6 2-3.6 4.3 0 1.1.3 1.9.7 2.6-1.7 1-2.9 3.4-2.9 6.4 0 4.6 2.6 7.7 5.8 7.7s5.8-3.1 5.8-7.7c0-3-1.2-5.4-2.9-6.4.4-.7.7-1.5.7-2.6C15.6 4 14.2 2 12 2Z" fill="#0B0B0B"/>
            <ellipse cx="12" cy="15.6" rx="3.1" ry="4.2" fill="#FFFFFF"/>
            <circle cx="10.1" cy="8.7" r="1.15" fill="#FFFFFF"/>
            <circle cx="13.9" cy="8.7" r="1.15" fill="#FFFFFF"/>
            <circle cx="10.35" cy="9" r="0.5" fill="#0B0B0B"/>
            <circle cx="13.65" cy="9" r="0.5" fill="#0B0B0B"/>
            <path d="M11.1 10.7h1.8l-.9 1.05Z" fill="#F5A623"/>
            <path d="M9 21.4c-.6 1-1.8 1.2-2.7 1-.3-.05-.3-.5 0-.6 1-.3 1.7-.9 2-1.7Z" fill="#F5A623"/>
            <path d="M15 21.4c.6 1 1.8 1.2 2.7 1 .3-.05.3-.5 0-.6-1-.3-1.7-.9-2-1.7Z" fill="#F5A623"/>
        </svg>
        @break
    @default
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="{{ $class }}">
            <rect x="3" y="4" width="18" height="13" rx="2"/>
            <path stroke-linecap="round" d="M8 20h8M12 17v3"/>
        </svg>
@endswitch
