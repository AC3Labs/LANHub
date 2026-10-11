<?php

namespace App\Console\Commands;

use App\Models\User;
use Illuminate\Console\Command;

/**
 * Last-resort recovery for someone who lost both their phone and their
 * recovery codes. Removes their authenticator app so their next login goes
 * back to an emailed code (and they can set the app up again afterward).
 * Needs shell access to the server, which is the trust boundary here.
 */
class ResetAuthenticator extends Command
{
    protected $signature = 'lanhub:reset-authenticator {email : The user whose authenticator app should be removed}';

    protected $description = "Remove a user's authenticator app (for a lost phone)";

    public function handle(): int
    {
        $user = User::where('email', $this->argument('email'))->first();

        if (! $user) {
            $this->error('No user with that email.');

            return self::FAILURE;
        }

        if (! $user->two_factor_secret) {
            $this->info("{$user->email} doesn't have an authenticator app set up. Nothing to do.");

            return self::SUCCESS;
        }

        $user->removeAuthenticator();

        $this->info("Removed the authenticator app for {$user->email}. Their next login uses an emailed code.");

        return self::SUCCESS;
    }
}
