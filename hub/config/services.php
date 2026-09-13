<?php

return [

    /*
    |--------------------------------------------------------------------------
    | Third Party Services
    |--------------------------------------------------------------------------
    |
    | This file is for storing the credentials for third party services such
    | as Resend, Postmark, AWS, and more. This file provides the de facto
    | location for this type of information, allowing packages to have
    | a conventional file to locate the various service credentials.
    |
    */

    'postmark' => [
        'key' => env('POSTMARK_API_KEY'),
    ],

    'resend' => [
        'key' => env('RESEND_API_KEY'),
    ],

    'ses' => [
        'key' => env('AWS_ACCESS_KEY_ID'),
        'secret' => env('AWS_SECRET_ACCESS_KEY'),
        'region' => env('AWS_DEFAULT_REGION', 'us-east-1'),
    ],

    'slack' => [
        'notifications' => [
            'bot_user_oauth_token' => env('SLACK_BOT_USER_OAUTH_TOKEN'),
            'channel' => env('SLACK_BOT_USER_DEFAULT_CHANNEL'),
        ],
    ],

    // ntfy.sh: a free, no-signup push notification service — pick a
    // random/hard-to-guess topic name (ntfy topics are public-by-obscurity,
    // anyone who knows the topic can read it) and subscribe to it in the
    // ntfy mobile app or at https://ntfy.sh/<topic> to receive alerts.
    // Used by App\Console\Commands\CheckMachineHealth to push a
    // notification the moment a registered machine goes offline.
    'ntfy' => [
        'topic' => env('NTFY_TOPIC'),
    ],

];
