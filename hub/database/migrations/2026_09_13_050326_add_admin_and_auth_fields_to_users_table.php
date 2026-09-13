<?php

use App\Models\User;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     *
     * Every existing user is backfilled as an admin — there's no other way
     * to bootstrap the first admin on an install that already has users,
     * same reasoning as the machine_user backfill in the access-control
     * migration.
     */
    public function up(): void
    {
        Schema::table('users', function (Blueprint $table) {
            $table->boolean('is_admin')->default(false)->after('password');
            // Set only for an invited-but-not-yet-activated user; cleared
            // once they set their password. Reused for both the initial
            // invite link and a manual "resend invite".
            $table->string('invite_token')->nullable()->unique()->after('is_admin');
            $table->timestamp('invite_token_expires_at')->nullable()->after('invite_token');
            // Hashed, never stored in plain text — matches how passwords
            // are stored, just with a short expiry instead of forever.
            $table->string('two_factor_code')->nullable()->after('invite_token_expires_at');
            $table->timestamp('two_factor_expires_at')->nullable()->after('two_factor_code');
        });

        User::query()->update(['is_admin' => true]);
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('users', function (Blueprint $table) {
            $table->dropColumn([
                'is_admin',
                'invite_token',
                'invite_token_expires_at',
                'two_factor_code',
                'two_factor_expires_at',
            ]);
        });
    }
};
