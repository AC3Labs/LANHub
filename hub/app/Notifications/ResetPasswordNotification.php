<?php

namespace App\Notifications;

use Illuminate\Bus\Queueable;
use Illuminate\Mail\Mailable;
use Illuminate\Notifications\Notification;

/**
 * Replaces Laravel's default (generic, unbranded) password-reset email —
 * see User::sendPasswordResetNotification() — with one that matches the
 * rest of LANHub's transactional email look (resources/views/emails/).
 * Sent synchronously, same reasoning as TwoFactorCodeMail/UserInvitation.
 */
class ResetPasswordNotification extends Notification
{
    use Queueable;

    public function __construct(public readonly string $token)
    {
        //
    }

    public function via(object $notifiable): array
    {
        return ['mail'];
    }

    public function toMail(object $notifiable): Mailable
    {
        $url = url(route('password.reset', [
            'token' => $this->token,
            'email' => $notifiable->getEmailForPasswordReset(),
        ], false));

        return (new Mailable)
            ->subject('Reset your '.config('app.name').' password')
            ->view('emails.reset-password', ['url' => $url]);
    }
}
