<?php

namespace App\Console\Commands;

use App\Models\User;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Validator;

/**
 * The only way to get a first account on a fresh install — self-registration
 * was deliberately removed (LANHub is invite-only, see CHANGELOG 0.5.0.0),
 * and every other path to a new user (the Users page, invite emails)
 * requires an existing admin to already be logged in. Without this command,
 * a fresh clone would have zero users and no way to ever log in at all.
 */
class CreateAdmin extends Command
{
    protected $signature = 'lanhub:create-admin';

    protected $description = 'Create the first admin user on a fresh install';

    public function handle(): int
    {
        if (User::query()->exists()) {
            $this->error('A user already exists. This command is only for bootstrapping a brand new install.');
            $this->line('To add more people, log in and use the Users page instead.');

            return self::FAILURE;
        }

        $name = $this->ask('Name');
        $email = $this->ask('Email');
        $password = $this->secret('Password (min 8 characters)');

        $validator = Validator::make(
            compact('name', 'email', 'password'),
            [
                'name' => ['required', 'string', 'max:255'],
                'email' => ['required', 'string', 'email', 'max:255'],
                'password' => ['required', 'string', 'min:8'],
            ]
        );

        if ($validator->fails()) {
            foreach ($validator->errors()->all() as $error) {
                $this->error($error);
            }

            return self::FAILURE;
        }

        // is_admin isn't in User's #[Fillable(...)] list (mass assignment
        // there is deliberately limited to name/email/password), so it
        // has to be set via forceFill instead of create().
        User::create(['name' => $name, 'email' => $email, 'password' => Hash::make($password)])
            ->forceFill(['is_admin' => true, 'email_verified_at' => now()])
            ->save();

        $this->info("Admin account created for {$email}. You can log in now.");

        return self::SUCCESS;
    }
}
