<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     *
     * A generic key/value store, starting with SMTP settings — lets an
     * admin configure real mail delivery from the Settings page instead
     * of editing .env (which, on the Docker deployment, is a bind-mounted
     * single file outside the app entirely). `value` is encrypted for
     * every row, not just the password ones — simpler than special-casing
     * which keys are sensitive, and cheap.
     */
    public function up(): void
    {
        Schema::create('settings', function (Blueprint $table) {
            $table->id();
            $table->string('key')->unique();
            $table->text('value')->nullable();
            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('settings');
    }
};
