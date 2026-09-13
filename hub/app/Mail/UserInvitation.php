<?php

namespace App\Mail;

use App\Models\User;
use Illuminate\Bus\Queueable;
use Illuminate\Mail\Mailable;
use Illuminate\Mail\Mailables\Content;
use Illuminate\Mail\Mailables\Envelope;
use Illuminate\Queue\SerializesModels;

/**
 * Sent synchronously, not queued — deliberately, unlike RelayTransferJob:
 * the recipient is often waiting right now for this link, and this
 * project's queue worker is a background loop with no monitoring, not
 * something an onboarding email should depend on.
 */
class UserInvitation extends Mailable
{
    use Queueable, SerializesModels;

    public function __construct(public readonly User $user)
    {
        //
    }

    public function envelope(): Envelope
    {
        return new Envelope(
            subject: "You've been invited to LANHub",
        );
    }

    public function content(): Content
    {
        return new Content(
            view: 'emails.user-invitation',
            with: [
                'name' => $this->user->name,
                'setPasswordUrl' => url('/set-password/'.$this->user->invite_token),
            ],
        );
    }
}
