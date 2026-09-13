import defaultTheme from 'tailwindcss/defaultTheme';
import forms from '@tailwindcss/forms';

/** @type {import('tailwindcss').Config} */
export default {
    content: [
        './vendor/laravel/framework/src/Illuminate/Pagination/resources/views/*.blade.php',
        './storage/framework/views/*.php',
        './resources/views/**/*.blade.php',
    ],

    // Dark mode toggled via a `dark` class on <html> (see resources/js/dark-mode.js
    // and the toggle in layout/navigation.blade.php) rather than the OS
    // preference alone, so the user's choice sticks.
    darkMode: 'class',

    theme: {
        extend: {
            fontFamily: {
                sans: ['Figtree', ...defaultTheme.fontFamily.sans],
            },
            // Every shade is a CSS custom property (see resources/css/app.css)
            // instead of a static hex value, re-pointed under `.dark` — this
            // is what makes dark mode work everywhere `bg-stone-100`,
            // `text-stone-800`, etc. are already used, with no changes
            // needed to any individual Blade view. The `rgb(var(...) /
            // <alpha-value>)` form is required for Tailwind's opacity
            // modifiers (e.g. `bg-stone-50/80`) to keep working.
            colors: {
                paper: {
                    DEFAULT: 'rgb(var(--color-paper) / <alpha-value>)',
                    soft: 'rgb(var(--color-paper-soft) / <alpha-value>)',
                },
                stone: {
                    50: 'rgb(var(--color-stone-50) / <alpha-value>)',
                    100: 'rgb(var(--color-stone-100) / <alpha-value>)',
                    200: 'rgb(var(--color-stone-200) / <alpha-value>)',
                    300: 'rgb(var(--color-stone-300) / <alpha-value>)',
                    400: 'rgb(var(--color-stone-400) / <alpha-value>)',
                    500: 'rgb(var(--color-stone-500) / <alpha-value>)',
                    600: 'rgb(var(--color-stone-600) / <alpha-value>)',
                    700: 'rgb(var(--color-stone-700) / <alpha-value>)',
                    800: 'rgb(var(--color-stone-800) / <alpha-value>)',
                    900: 'rgb(var(--color-stone-900) / <alpha-value>)',
                },
                tan: {
                    50: 'rgb(var(--color-tan-50) / <alpha-value>)',
                    100: 'rgb(var(--color-tan-100) / <alpha-value>)',
                    200: 'rgb(var(--color-tan-200) / <alpha-value>)',
                    300: 'rgb(var(--color-tan-300) / <alpha-value>)',
                    400: 'rgb(var(--color-tan-400) / <alpha-value>)',
                    500: 'rgb(var(--color-tan-500) / <alpha-value>)',
                    600: 'rgb(var(--color-tan-600) / <alpha-value>)',
                    700: 'rgb(var(--color-tan-700) / <alpha-value>)',
                    800: 'rgb(var(--color-tan-800) / <alpha-value>)',
                    900: 'rgb(var(--color-tan-900) / <alpha-value>)',
                },
            },
            boxShadow: {
                soft: '0 1px 2px 0 rgb(44 41 37 / 0.05), 0 1px 3px 0 rgb(44 41 37 / 0.06)',
                panel: '0 1px 3px 0 rgb(44 41 37 / 0.06), 0 4px 12px -2px rgb(44 41 37 / 0.06)',
            },
        },
    },

    plugins: [forms],
};
