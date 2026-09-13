<?php

namespace App\Mail;

use Illuminate\Bus\Queueable;
use Illuminate\Mail\Mailable;
use Illuminate\Mail\Mailables\Content;
use Illuminate\Mail\Mailables\Envelope;
use Illuminate\Queue\SerializesModels;

/**
 * Sent synchronously — the recipient is actively waiting on a login
 * screen for this, so it must not depend on a queue worker running.
 */
class TwoFactorCodeMail extends Mailable
{
    use Queueable, SerializesModels;

    public function __construct(public readonly string $code)
    {
        //
    }

    public function envelope(): Envelope
    {
        return new Envelope(
            subject: 'Your LANHub login code',
        );
    }

    public function content(): Content
    {
        return new Content(
            view: 'emails.two-factor-code',
            with: ['code' => $this->code],
        );
    }
}
